package engine

import (
	"crypto/sha1"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SimonMing47/BitTorrent/internal/manifest"
	"github.com/SimonMing47/BitTorrent/internal/peerwire"
)

func TestCatalogLeaseReleaseAndComplete(t *testing.T) {
	meta := manifest.Manifest{
		TotalLength:         9,
		StandardPieceLength: 4,
		PieceDigests:        make([][20]byte, 3),
	}

	book := newCatalog(meta)
	bitmap := peerwire.Bitmap{0b1110_0000}

	first, ok, done := book.TryLease(bitmap, meta)
	if !ok || done {
		t.Fatalf("expected first lease, got ok=%v done=%v", ok, done)
	}
	if first.Index != 0 || first.Length != 4 || first.Offset != 0 {
		t.Fatalf("unexpected first lease: %+v", first)
	}

	second, ok, done := book.TryLease(bitmap, meta)
	if !ok || done {
		t.Fatalf("expected second lease, got ok=%v done=%v", ok, done)
	}
	if second.Index != 1 || second.Length != 4 || second.Offset != 4 {
		t.Fatalf("unexpected second lease: %+v", second)
	}

	book.Release(first.Index)
	released, ok, done := book.TryLease(bitmap, meta)
	if !ok || done || released.Index != 0 {
		t.Fatalf("expected released piece to be available again, got %+v ok=%v done=%v", released, ok, done)
	}

	book.MarkDone(released.Index)
	book.MarkDone(second.Index)
	last, ok, done := book.TryLease(bitmap, meta)
	if !ok || done {
		t.Fatalf("expected final lease, got %+v ok=%v done=%v", last, ok, done)
	}
	if last.Index != 2 || last.Length != 1 || last.Offset != 8 {
		t.Fatalf("unexpected last lease: %+v", last)
	}

	book.MarkDone(last.Index)
	if _, ok, done := book.TryLease(bitmap, meta); ok || !done {
		t.Fatalf("expected catalog to be done, got ok=%v done=%v", ok, done)
	}
}

func TestSelectAuditPieces(t *testing.T) {
	indexes := selectAuditPieces(10, 4)
	expected := []int{0, 3, 6, 9}
	if !reflect.DeepEqual(indexes, expected) {
		t.Fatalf("unexpected audit indexes: got %v want %v", indexes, expected)
	}
}

func TestNewCatalogWithPending(t *testing.T) {
	meta := manifest.Manifest{
		TotalLength:         12,
		StandardPieceLength: 4,
		PieceDigests:        make([][20]byte, 3),
	}

	book := newCatalogWithPending(meta, []int{1})
	bitmap := peerwire.Bitmap{0b1110_0000}

	lease, ok, done := book.TryLease(bitmap, meta)
	if !ok || done {
		t.Fatalf("expected one pending piece, got ok=%v done=%v", ok, done)
	}
	if lease.Index != 1 {
		t.Fatalf("unexpected pending piece index: %d", lease.Index)
	}

	book.MarkDone(lease.Index)
	if _, ok, done := book.TryLease(bitmap, meta); ok || !done {
		t.Fatalf("expected repair catalog to finish, got ok=%v done=%v", ok, done)
	}
}

func TestAuditDownloadedPieces(t *testing.T) {
	payload := []byte("abcdefghijklmnop")
	piece0 := sha1.Sum(payload[:8])
	piece1 := sha1.Sum(payload[8:])

	meta := manifest.Manifest{
		TotalLength:         int64(len(payload)),
		StandardPieceLength: 8,
		PieceDigests:        [][20]byte{piece0, piece1},
	}

	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	checked, failed, err := auditDownloadedPieces(path, meta, 1)
	if err != nil {
		t.Fatalf("auditDownloadedPieces() error = %v", err)
	}
	if checked != 1 {
		t.Fatalf("unexpected checked count: %d", checked)
	}
	if failed != -1 {
		t.Fatalf("unexpected failed piece index: %d", failed)
	}
}

func TestCollectCorruptPieces(t *testing.T) {
	payload := []byte("abcdefghijklmnop")
	piece0 := sha1.Sum(payload[:8])
	piece1 := sha1.Sum(payload[8:])

	meta := manifest.Manifest{
		TotalLength:         int64(len(payload)),
		StandardPieceLength: 8,
		PieceDigests:        [][20]byte{piece0, piece1},
	}

	path := filepath.Join(t.TempDir(), "payload.bin")
	corrupt := append([]byte(nil), payload...)
	corrupt[9] = 'Z'
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	indexes, err := collectCorruptPieces(path, meta, []int{0, 1})
	if err != nil {
		t.Fatalf("collectCorruptPieces() error = %v", err)
	}

	expected := []int{1}
	if !reflect.DeepEqual(indexes, expected) {
		t.Fatalf("unexpected corrupt indexes: got %v want %v", indexes, expected)
	}
}
