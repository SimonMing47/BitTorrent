package peerwire

import (
	"bytes"
	"testing"
)

func TestGreetingEncodeDecode(t *testing.T) {
	greeting := NewGreeting(
		[20]byte{1, 2, 3},
		[20]byte{4, 5, 6},
	)

	decoded, err := ReadGreeting(bytes.NewReader(greeting.Encode()))
	if err != nil {
		t.Fatalf("ReadGreeting() error = %v", err)
	}
	if decoded.InfoHash != greeting.InfoHash {
		t.Fatalf("info hash mismatch: %x vs %x", decoded.InfoHash, greeting.InfoHash)
	}
	if decoded.PeerID != greeting.PeerID {
		t.Fatalf("peer id mismatch: %x vs %x", decoded.PeerID, greeting.PeerID)
	}
}

func TestBitmap(t *testing.T) {
	bits := Bitmap{0b1010_0000}
	if !bits.Contains(0) || bits.Contains(1) {
		t.Fatalf("unexpected Contains() result for initial bitmap")
	}
	bits.Mark(1)
	if !bits.Contains(1) {
		t.Fatal("Mark() did not flip the requested bit")
	}
}

func TestPacketReadAndEncode(t *testing.T) {
	packet := RequestPacket(7, 32, 4096)
	decoded, err := ReadPacket(bytes.NewReader(packet.Encode()))
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}

	piece, offset, length, err := ParseRequest(decoded)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}
	if piece != 7 || offset != 32 || length != 4096 {
		t.Fatalf("unexpected request values: %d %d %d", piece, offset, length)
	}
}

func TestReadKeepAlive(t *testing.T) {
	packet, err := ReadPacket(bytes.NewReader([]byte{0, 0, 0, 0}))
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if !packet.KeepAlive {
		t.Fatal("expected keepalive packet")
	}
}

func TestParseHave(t *testing.T) {
	index, err := ParseHave(HavePacket(12))
	if err != nil {
		t.Fatalf("ParseHave() error = %v", err)
	}
	if index != 12 {
		t.Fatalf("unexpected have index: %d", index)
	}
}

func TestCopyBlock(t *testing.T) {
	buffer := make([]byte, 10)
	packet := PiecePacket(2, 4, []byte{9, 8, 7})

	wrote, err := CopyBlock(packet, 2, buffer)
	if err != nil {
		t.Fatalf("CopyBlock() error = %v", err)
	}
	if wrote != 3 {
		t.Fatalf("unexpected byte count: %d", wrote)
	}
	expected := []byte{0, 0, 0, 0, 9, 8, 7, 0, 0, 0}
	if !bytes.Equal(expected, buffer) {
		t.Fatalf("unexpected buffer: %v", buffer)
	}
}
