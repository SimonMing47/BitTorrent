package discovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/mac/bt-refractor/internal/bencode"
)

// Endpoint 表示 tracker 返回的一个 peer 地址。
type Endpoint struct {
	Address net.IP
	Port    uint16
}

func (e Endpoint) String() string {
	return net.JoinHostPort(e.Address.String(), strconv.Itoa(int(e.Port)))
}

// AnnounceRequest 表示一次 HTTP announce 所需的查询参数。
type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       uint16
	Uploaded   int64
	Downloaded int64
	Left       int64
	Compact    bool
}

// AnnounceReply 表示当前下载器真正关心的 tracker 响应字段。
type AnnounceReply struct {
	Interval time.Duration
	Peers    []Endpoint
}

// Options 控制 tracker 的 HTTP/HTTPS 连接建立方式。
type Options struct {
	Timeout         time.Duration
	CertificatePath string
	SkipTLSVerify   bool
}

// HTTPClient 负责向 HTTP 或 HTTPS tracker 发起 announce 请求。
type HTTPClient struct {
	Client *http.Client
}

// New 使用默认参数创建一个 tracker 客户端。
func New(client *http.Client) *HTTPClient {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &HTTPClient{Client: client}
}

// NewWithOptions 使用可配置的 TCP/TLS 拨号策略创建 tracker 客户端。
func NewWithOptions(options Options) (*HTTPClient, error) {
	client, err := newHTTPClient(options)
	if err != nil {
		return nil, err
	}
	return &HTTPClient{Client: client}, nil
}

// BuildURL 根据 announce 参数拼出最终请求 URL。
func BuildURL(raw string, req AnnounceRequest) (string, error) {
	base, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	compactValue := "0"
	if req.Compact {
		compactValue = "1"
	}

	query := url.Values{
		"compact":    []string{compactValue},
		"downloaded": []string{strconv.FormatInt(req.Downloaded, 10)},
		"info_hash":  []string{string(req.InfoHash[:])},
		"left":       []string{strconv.FormatInt(req.Left, 10)},
		"peer_id":    []string{string(req.PeerID[:])},
		"port":       []string{strconv.Itoa(int(req.Port))},
		"uploaded":   []string{strconv.FormatInt(req.Uploaded, 10)},
	}
	base.RawQuery = query.Encode()
	return base.String(), nil
}

// Announce 向指定 tracker 发起请求并解析 peers 列表。
func (c *HTTPClient) Announce(ctx context.Context, announceURL string, req AnnounceRequest) (AnnounceReply, error) {
	urlWithQuery, err := BuildURL(announceURL, req)
	if err != nil {
		return AnnounceReply{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, urlWithQuery, nil)
	if err != nil {
		return AnnounceReply{}, err
	}

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return AnnounceReply{}, err
	}
	defer resp.Body.Close()

	payload, err := bencode.Decode(resp.Body)
	if err != nil {
		return AnnounceReply{}, err
	}

	dict, ok := payload.(map[string]any)
	if !ok {
		return AnnounceReply{}, fmt.Errorf("tracker response must be a dictionary")
	}

	if failure, ok := dict["failure reason"]; ok {
		if text, ok := failure.([]byte); ok {
			return AnnounceReply{}, fmt.Errorf("tracker failure: %s", string(text))
		}
		return AnnounceReply{}, fmt.Errorf("tracker returned failure")
	}

	intervalRaw, ok := dict["interval"].(int64)
	if !ok {
		return AnnounceReply{}, fmt.Errorf("tracker response missing interval")
	}

	peerBlob, ok := dict["peers"].([]byte)
	if !ok {
		return AnnounceReply{}, fmt.Errorf("tracker response missing compact peer list")
	}
	peers, err := DecodeCompactPeers(peerBlob)
	if err != nil {
		return AnnounceReply{}, err
	}

	return AnnounceReply{
		Interval: time.Duration(intervalRaw) * time.Second,
		Peers:    peers,
	}, nil
}

// DecodeCompactPeers 解析 tracker 返回的 compact peers 二进制列表。
func DecodeCompactPeers(blob []byte) ([]Endpoint, error) {
	const compactPeerSize = 6
	if len(blob)%compactPeerSize != 0 {
		return nil, fmt.Errorf("compact peer list length %d is invalid", len(blob))
	}

	peers := make([]Endpoint, 0, len(blob)/compactPeerSize)
	for idx := 0; idx < len(blob); idx += compactPeerSize {
		peers = append(peers, Endpoint{
			Address: net.IP(blob[idx : idx+4]),
			Port:    uint16(blob[idx+4])<<8 | uint16(blob[idx+5]),
		})
	}
	return peers, nil
}

func newHTTPClient(options Options) (*http.Client, error) {
	if options.Timeout <= 0 {
		options.Timeout = 15 * time.Second
	}

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := net.Dialer{Timeout: options.Timeout}
		return dialer.DialContext(ctx, network, addr)
	}

	transport := &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: dialContext,
	}

	if options.CertificatePath != "" || options.SkipTLSVerify {
		tlsConfig, err := buildTLSConfig(options)
		if err != nil {
			return nil, err
		}
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}

			config := tlsConfig.Clone()
			if config.ServerName == "" {
				config.ServerName = host
			}

			dialer := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: options.Timeout},
				Config:    config,
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}

	return &http.Client{
		Timeout:   options.Timeout,
		Transport: transport,
	}, nil
}

func buildTLSConfig(options Options) (*tls.Config, error) {
	config := &tls.Config{
		InsecureSkipVerify: options.SkipTLSVerify,
		MinVersion:         tls.VersionTLS12,
	}

	if options.CertificatePath == "" {
		return config, nil
	}

	pemData, err := os.ReadFile(options.CertificatePath)
	if err != nil {
		return nil, fmt.Errorf("read tracker certificate: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("tracker certificate %q is not valid PEM", options.CertificatePath)
	}
	config.RootCAs = pool
	return config, nil
}
