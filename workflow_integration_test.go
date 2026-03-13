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

	"github.com/mac/bt-refractor/internal/bencode"
	"github.com/mac/bt-refractor/internal/discovery"
	"github.com/mac/bt-refractor/internal/engine"
	"github.com/mac/bt-refractor/internal/manifest"
	"github.com/mac/bt-refractor/internal/peerwire"
)

func TestEndToEndDownload(t *testing.T) {
	payload := []byte("BitTorrent rewrite from scratch in Go.")
	pieceLength := 12
	pieceBlob := buildPieceBlob(payload, pieceLength)

	info := map[string]any{
		"length":       int64(len(payload)),
		"name":         []byte("rewrite.bin"),
		"piece length": int64(pieceLength),
		"pieces":       pieceBlob,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal(info) error = %v", err)
	}
	infoHash := sha1.Sum(infoBytes)
	go servePeer(t, listener, infoHash, payload, pieceLength)

	trackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tcpAddr := listener.Addr().(*net.TCPAddr)
		response, err := bencode.Marshal(map[string]any{
			"interval": int64(60),
			"peers": []byte{
				127, 0, 0, 1,
				byte(tcpAddr.Port >> 8), byte(tcpAddr.Port),
			},
		})
		if err != nil {
			t.Fatalf("Marshal(response) error = %v", err)
		}
		_, _ = w.Write(response)
	}))
	defer trackerServer.Close()

	torrentBytes, err := bencode.Marshal(map[string]any{
		"announce": []byte(trackerServer.URL),
		"info":     info,
	})
	if err != nil {
		t.Fatalf("Marshal(torrent) error = %v", err)
	}

	dir := t.TempDir()
	torrentPath := filepath.Join(dir, "fixture.torrent")
	outputPath := filepath.Join(dir, "output.bin")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	meta, err := manifest.Load(torrentPath)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}

	peerID := [20]byte{45, 66, 82, 48, 48, 48, 49, 45, 1, 2, 3}
	reply, err := discovery.New(nil).Announce(context.Background(), meta.Announce, discovery.AnnounceRequest{
		InfoHash: meta.InfoHash,
		PeerID:   peerID,
		Port:     6881,
		Left:     meta.TotalLength,
		Compact:  true,
	})
	if err != nil {
		t.Fatalf("tracker announce error = %v", err)
	}

	manager := engine.New(meta, reply.Peers, peerID, log.New(io.Discard, "", 0), engine.Settings{
		DialTimeout:   time.Second,
		IOTimeout:     2 * time.Second,
		BlockSize:     6,
		PipelineDepth: 2,
	})
	if err := manager.Save(context.Background(), outputPath); err != nil {
		t.Fatalf("manager.Save() error = %v", err)
	}

	downloaded, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(downloaded) != string(payload) {
		t.Fatalf("download mismatch:\nwant %q\ngot  %q", payload, downloaded)
	}
}

func buildPieceBlob(payload []byte, pieceLength int) []byte {
	blob := make([]byte, 0, ((len(payload)+pieceLength-1)/pieceLength)*20)
	for offset := 0; offset < len(payload); offset += pieceLength {
		end := offset + pieceLength
		if end > len(payload) {
			end = len(payload)
		}
		sum := sha1.Sum(payload[offset:end])
		blob = append(blob, sum[:]...)
	}
	return blob
}

func servePeer(t *testing.T, listener net.Listener, infoHash [20]byte, payload []byte, pieceLength int) {
	t.Helper()

	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	greeting, err := peerwire.ReadGreeting(conn)
	if err != nil {
		t.Errorf("ReadGreeting() error = %v", err)
		return
	}
	if greeting.InfoHash != infoHash {
		t.Errorf("unexpected info hash: %x", greeting.InfoHash)
		return
	}

	remoteID := [20]byte{45, 70, 65, 75, 69, 45, 80, 69, 69, 82}
	if _, err := conn.Write(peerwire.NewGreeting(infoHash, remoteID).Encode()); err != nil {
		t.Errorf("Write(greeting) error = %v", err)
		return
	}

	pieceCount := (len(payload) + pieceLength - 1) / pieceLength
	bitfield := make(peerwire.Bitmap, (pieceCount+7)/8)
	for index := 0; index < pieceCount; index++ {
		bitfield.Mark(index)
	}
	if _, err := conn.Write(peerwire.Packet{Kind: peerwire.KindBitfield, Payload: bitfield}.Encode()); err != nil {
		t.Errorf("Write(bitfield) error = %v", err)
		return
	}

	for {
		packet, err := peerwire.ReadPacket(conn)
		if err != nil {
			return
		}
		if packet.KeepAlive {
			continue
		}

		switch packet.Kind {
		case peerwire.KindInterested:
			if _, err := conn.Write(peerwire.Control(peerwire.KindUnchoke).Encode()); err != nil {
				t.Errorf("Write(unchoke) error = %v", err)
				return
			}
		case peerwire.KindRequest:
			pieceIndex, begin, length, err := peerwire.ParseRequest(packet)
			if err != nil {
				t.Errorf("ParseRequest() error = %v", err)
				return
			}
			absolute := pieceIndex*pieceLength + begin
			if absolute+length > len(payload) {
				length = len(payload) - absolute
			}
			block := append([]byte(nil), payload[absolute:absolute+length]...)
			if _, err := conn.Write(peerwire.PiecePacket(pieceIndex, begin, block).Encode()); err != nil {
				t.Errorf("Write(piece) error = %v", err)
				return
			}
		case peerwire.KindHave:
		}
	}
}
