package metainfo

import (
	"bytes"
	"crypto/sha1"
	"os"
	"path/filepath"
	"testing"

	"github.com/mac/bt-refractor/internal/bencode"
)

func TestParse(t *testing.T) {
	firstPiece := sha1.Sum([]byte("abcdefgh"))
	secondPiece := sha1.Sum([]byte("ijklm"))
	info := map[string]any{
		"length":       int64(13),
		"name":         []byte("sample.bin"),
		"piece length": int64(8),
		"pieces": append(
			firstPiece[:],
			secondPiece[:]...,
		),
	}

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal(info) error = %v", err)
	}

	torrentBytes, err := bencode.Marshal(map[string]any{
		"announce": []byte("http://tracker.local/announce"),
		"info":     info,
	})
	if err != nil {
		t.Fatalf("Marshal(torrent) error = %v", err)
	}

	manifest, err := Parse(torrentBytes)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if manifest.Announce != "http://tracker.local/announce" {
		t.Fatalf("unexpected announce: %q", manifest.Announce)
	}
	if manifest.Name != "sample.bin" {
		t.Fatalf("unexpected name: %q", manifest.Name)
	}
	if manifest.TotalLength != 13 {
		t.Fatalf("unexpected length: %d", manifest.TotalLength)
	}
	if manifest.StandardPieceLength != 8 {
		t.Fatalf("unexpected piece length: %d", manifest.StandardPieceLength)
	}
	if manifest.PieceCount() != 2 {
		t.Fatalf("unexpected piece count: %d", manifest.PieceCount())
	}

	expectedHash := sha1.Sum(infoBytes)
	if manifest.InfoHash != expectedHash {
		t.Fatalf("unexpected info hash: %x", manifest.InfoHash)
	}

	offset, length, err := manifest.PieceSpan(1)
	if err != nil {
		t.Fatalf("PieceSpan() error = %v", err)
	}
	if offset != 8 || length != 5 {
		t.Fatalf("unexpected second piece span: offset=%d length=%d", offset, length)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.torrent")
	data, err := bencode.Marshal(map[string]any{
		"announce": []byte("http://tracker"),
		"info": map[string]any{
			"length":       int64(1),
			"name":         []byte("x"),
			"piece length": int64(1),
			"pieces":       bytes.Repeat([]byte("a"), 20),
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manifest, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if manifest.Name != "x" {
		t.Fatalf("unexpected name: %q", manifest.Name)
	}
}

func TestParseRejectsMultifileTorrents(t *testing.T) {
	torrentBytes, err := bencode.Marshal(map[string]any{
		"announce": []byte("http://tracker.local/announce"),
		"info": map[string]any{
			"name":         []byte("folder"),
			"piece length": int64(8),
			"pieces":       bytes.Repeat([]byte{1}, 20),
			"files":        []any{},
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if _, err := Parse(torrentBytes); err == nil {
		t.Fatal("expected multifile torrent to be rejected")
	}
}
