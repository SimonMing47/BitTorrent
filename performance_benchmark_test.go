package bt_test

import (
	"context"
	"crypto/sha1"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SimonMing47/BitTorrent/internal/bencode"
	"github.com/SimonMing47/BitTorrent/internal/discovery"
	"github.com/SimonMing47/BitTorrent/internal/engine"
	"github.com/SimonMing47/BitTorrent/internal/manifest"
)

func BenchmarkDownloadModes(b *testing.B) {
	swarm := newBenchmarkSwarm(b, 6, 32<<20, 256<<10, 0)
	defer swarm.Close()

	meta, err := manifest.Load(swarm.torrentPath)
	if err != nil {
		b.Fatalf("manifest.Load() error = %v", err)
	}

	peerID := [20]byte{45, 66, 82, 48, 48, 48, 49, 45, 7, 7, 7}
	reply, err := discovery.New(nil).Announce(context.Background(), meta.Announce, discovery.AnnounceRequest{
		InfoHash: meta.InfoHash,
		PeerID:   peerID,
		Port:     6881,
		Left:     meta.TotalLength,
		Compact:  true,
	})
	if err != nil {
		b.Fatalf("tracker announce error = %v", err)
	}

	cases := []struct {
		name     string
		settings engine.Settings
	}{
		{
			name: "original_like_strict_p5",
			settings: engine.Settings{
				DialTimeout:   time.Second,
				IOTimeout:     3 * time.Second,
				BlockSize:     16 * 1024,
				PipelineDepth: 5,
				VerifyPieces:  true,
			},
		},
		{
			name: "strict_p64",
			settings: engine.Settings{
				DialTimeout:   time.Second,
				IOTimeout:     3 * time.Second,
				BlockSize:     16 * 1024,
				PipelineDepth: 64,
				VerifyPieces:  true,
			},
		},
		{
			name: "datacenter_fast_p64",
			settings: engine.Settings{
				DialTimeout:   time.Second,
				IOTimeout:     3 * time.Second,
				BlockSize:     16 * 1024,
				PipelineDepth: 64,
				AuditPieces:   32,
				RepairRounds:  3,
			},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			outputDir := b.TempDir()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				outputPath := filepath.Join(outputDir, tc.name+"-"+itoa(i)+".bin")
				manager := engine.New(meta, reply.Peers, peerID, log.New(io.Discard, "", 0), tc.settings)
				if err := manager.Save(context.Background(), outputPath); err != nil {
					b.Fatalf("manager.Save() error = %v", err)
				}
				if err := os.Remove(outputPath); err != nil {
					b.Fatalf("os.Remove() error = %v", err)
				}
			}
		})
	}
}

type benchmarkSwarm struct {
	torrentPath string
	tracker     *httptest.Server
	listeners   []net.Listener
}

func newBenchmarkSwarm(tb testing.TB, peerCount, payloadSize, pieceLength int, responseDelay time.Duration) *benchmarkSwarm {
	tb.Helper()

	payload := make([]byte, payloadSize)
	for index := range payload {
		payload[index] = byte(index % 251)
	}
	pieceBlob := buildPieceBlob(payload, pieceLength)

	info := map[string]any{
		"length":       int64(len(payload)),
		"name":         []byte("benchmark.bin"),
		"piece length": int64(pieceLength),
		"pieces":       pieceBlob,
	}

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		tb.Fatalf("Marshal(info) error = %v", err)
	}
	infoHash := sha1.Sum(infoBytes)

	listeners := make([]net.Listener, 0, peerCount)
	for i := 0; i < peerCount; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			tb.Fatalf("net.Listen() error = %v", err)
		}
		listeners = append(listeners, listener)
		go servePeerLoop(tb, listener, infoHash, payload, pieceLength, peerBehavior{
			ResponseDelay: responseDelay,
		})
	}

	trackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		compactPeers := make([]byte, 0, len(listeners)*6)
		for _, listener := range listeners {
			addr := listener.Addr().(*net.TCPAddr)
			compactPeers = append(compactPeers,
				127, 0, 0, 1,
				byte(addr.Port>>8), byte(addr.Port),
			)
		}
		response, err := bencode.Marshal(map[string]any{
			"interval": int64(60),
			"peers":    compactPeers,
		})
		if err != nil {
			tb.Fatalf("Marshal(response) error = %v", err)
		}
		_, _ = w.Write(response)
	}))

	torrentBytes, err := bencode.Marshal(map[string]any{
		"announce": []byte(trackerServer.URL),
		"info":     info,
	})
	if err != nil {
		tb.Fatalf("Marshal(torrent) error = %v", err)
	}

	dir := tb.TempDir()
	torrentPath := filepath.Join(dir, "benchmark.torrent")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		tb.Fatalf("WriteFile() error = %v", err)
	}

	return &benchmarkSwarm{
		torrentPath: torrentPath,
		tracker:     trackerServer,
		listeners:   listeners,
	}
}

func (s *benchmarkSwarm) Close() {
	if s.tracker != nil {
		s.tracker.Close()
	}
	for _, listener := range s.listeners {
		_ = listener.Close()
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	var buf [20]byte
	index := len(buf)
	for value > 0 {
		index--
		buf[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[index:])
}
