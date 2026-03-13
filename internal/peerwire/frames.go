package peerwire

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Kind is the numeric message code used on the peer wire.
type Kind byte

const (
	KindChoke Kind = iota
	KindUnchoke
	KindInterested
	KindNotInterested
	KindHave
	KindBitfield
	KindRequest
	KindPiece
	KindCancel
)

// Packet is a single peer message or keepalive.
type Packet struct {
	Kind      Kind
	Payload   []byte
	KeepAlive bool
}

// Encode serializes a packet into its length-prefixed wire format.
func (p Packet) Encode() []byte {
	if p.KeepAlive {
		return make([]byte, 4)
	}
	length := uint32(len(p.Payload) + 1)
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[:4], length)
	buf[4] = byte(p.Kind)
	copy(buf[5:], p.Payload)
	return buf
}

// ReadPacket decodes a packet from the network stream.
func ReadPacket(r io.Reader) (Packet, error) {
	var lengthPrefix [4]byte
	if _, err := io.ReadFull(r, lengthPrefix[:]); err != nil {
		return Packet{}, err
	}
	size := binary.BigEndian.Uint32(lengthPrefix[:])
	if size == 0 {
		return Packet{KeepAlive: true}, nil
	}

	frame := make([]byte, size)
	if _, err := io.ReadFull(r, frame); err != nil {
		return Packet{}, err
	}
	return Packet{
		Kind:    Kind(frame[0]),
		Payload: frame[1:],
	}, nil
}

// Control constructs a payload-less control packet.
func Control(kind Kind) Packet {
	return Packet{Kind: kind}
}

// InterestedPacket expresses download interest to a peer.
func InterestedPacket() Packet {
	return Control(KindInterested)
}

// RequestPacket asks a peer for a block within a piece.
func RequestPacket(piece, offset, length int) Packet {
	body := make([]byte, 12)
	binary.BigEndian.PutUint32(body[0:4], uint32(piece))
	binary.BigEndian.PutUint32(body[4:8], uint32(offset))
	binary.BigEndian.PutUint32(body[8:12], uint32(length))
	return Packet{Kind: KindRequest, Payload: body}
}

// ParseRequest extracts the piece index, block offset, and block size.
func ParseRequest(packet Packet) (int, int, int, error) {
	if packet.Kind != KindRequest {
		return 0, 0, 0, fmt.Errorf("expected request packet, got %d", packet.Kind)
	}
	if len(packet.Payload) != 12 {
		return 0, 0, 0, fmt.Errorf("request payload must be 12 bytes, got %d", len(packet.Payload))
	}
	return int(binary.BigEndian.Uint32(packet.Payload[0:4])),
		int(binary.BigEndian.Uint32(packet.Payload[4:8])),
		int(binary.BigEndian.Uint32(packet.Payload[8:12])),
		nil
}

// HavePacket announces that a piece has been completed.
func HavePacket(piece int) Packet {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, uint32(piece))
	return Packet{Kind: KindHave, Payload: body}
}

// ParseHave extracts the piece index from a HAVE packet.
func ParseHave(packet Packet) (int, error) {
	if packet.Kind != KindHave {
		return 0, fmt.Errorf("expected have packet, got %d", packet.Kind)
	}
	if len(packet.Payload) != 4 {
		return 0, fmt.Errorf("have payload must be 4 bytes, got %d", len(packet.Payload))
	}
	return int(binary.BigEndian.Uint32(packet.Payload)), nil
}

// PiecePacket wraps block bytes for a piece response.
func PiecePacket(piece, offset int, block []byte) Packet {
	body := make([]byte, 8+len(block))
	binary.BigEndian.PutUint32(body[0:4], uint32(piece))
	binary.BigEndian.PutUint32(body[4:8], uint32(offset))
	copy(body[8:], block)
	return Packet{Kind: KindPiece, Payload: body}
}

// CopyBlock copies a PIECE payload into the destination buffer.
func CopyBlock(packet Packet, expectedPiece int, dest []byte) (int, error) {
	if packet.Kind != KindPiece {
		return 0, fmt.Errorf("expected piece packet, got %d", packet.Kind)
	}
	if len(packet.Payload) < 8 {
		return 0, fmt.Errorf("piece payload too short: %d", len(packet.Payload))
	}

	pieceIndex := int(binary.BigEndian.Uint32(packet.Payload[0:4]))
	if pieceIndex != expectedPiece {
		return 0, fmt.Errorf("expected piece %d, got %d", expectedPiece, pieceIndex)
	}

	offset := int(binary.BigEndian.Uint32(packet.Payload[4:8]))
	block := packet.Payload[8:]
	if offset < 0 || offset > len(dest) {
		return 0, fmt.Errorf("piece block offset %d out of bounds for %d bytes", offset, len(dest))
	}
	if offset+len(block) > len(dest) {
		return 0, fmt.Errorf("piece block length %d exceeds destination", len(block))
	}
	copy(dest[offset:], block)
	return len(block), nil
}
