package tracker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/mac/bt-refractor/internal/bencode"
)

func TestBuildURL(t *testing.T) {
	announceURL, err := BuildURL("http://tracker.local/announce", AnnounceRequest{
		InfoHash: [20]byte{1, 2, 3},
		PeerID:   [20]byte{4, 5, 6},
		Port:     6881,
		Left:     99,
		Compact:  true,
	})
	if err != nil {
		t.Fatalf("BuildURL() error = %v", err)
	}

	parsed, err := url.Parse(announceURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	query := parsed.Query()
	if query.Get("compact") != "1" {
		t.Fatalf("compact flag mismatch: %q", query.Get("compact"))
	}
	if query.Get("left") != "99" {
		t.Fatalf("left mismatch: %q", query.Get("left"))
	}
	if query.Get("port") != "6881" {
		t.Fatalf("port mismatch: %q", query.Get("port"))
	}
}

func TestAnnounce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := bencode.Marshal(map[string]any{
			"interval": int64(120),
			"peers": []byte{
				127, 0, 0, 1, 0x1A, 0xE1,
				10, 0, 0, 7, 0x1A, 0xE9,
			},
		})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	client := New(&http.Client{Timeout: time.Second})
	reply, err := client.Announce(context.Background(), server.URL, AnnounceRequest{
		InfoHash: [20]byte{1, 2, 3},
		PeerID:   [20]byte{4, 5, 6},
		Port:     6881,
		Left:     42,
		Compact:  true,
	})
	if err != nil {
		t.Fatalf("Announce() error = %v", err)
	}

	if reply.Interval != 120*time.Second {
		t.Fatalf("unexpected interval: %s", reply.Interval)
	}
	if len(reply.Peers) != 2 {
		t.Fatalf("unexpected peer count: %d", len(reply.Peers))
	}
	if !reply.Peers[0].Address.Equal(net.IPv4(127, 0, 0, 1)) || reply.Peers[0].Port != 6881 {
		t.Fatalf("unexpected first peer: %+v", reply.Peers[0])
	}
}

func TestDecodeCompactPeersRejectsMalformedData(t *testing.T) {
	if _, err := DecodeCompactPeers([]byte{127, 0, 0, 1, 0x1A}); err == nil {
		t.Fatal("expected malformed compact peer list to fail")
	}
}
