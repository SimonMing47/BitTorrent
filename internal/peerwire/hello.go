package peerwire

import (
	"fmt"
	"io"
)

const protocolName = "BitTorrent protocol"

// Greeting is the BitTorrent handshake frame.
type Greeting struct {
	Flags    [8]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

// NewGreeting creates a standard BitTorrent handshake.
func NewGreeting(infoHash, peerID [20]byte) Greeting {
	return Greeting{
		InfoHash: infoHash,
		PeerID:   peerID,
	}
}

// Encode serializes the handshake.
func (g Greeting) Encode() []byte {
	payload := make([]byte, len(protocolName)+49)
	payload[0] = byte(len(protocolName))
	cursor := 1
	cursor += copy(payload[cursor:], protocolName)
	cursor += copy(payload[cursor:], g.Flags[:])
	cursor += copy(payload[cursor:], g.InfoHash[:])
	copy(payload[cursor:], g.PeerID[:])
	return payload
}

// ReadGreeting reads a handshake from a stream.
func ReadGreeting(r io.Reader) (Greeting, error) {
	var lengthPrefix [1]byte
	if _, err := io.ReadFull(r, lengthPrefix[:]); err != nil {
		return Greeting{}, err
	}
	if lengthPrefix[0] == 0 {
		return Greeting{}, fmt.Errorf("protocol string length cannot be zero")
	}

	frame := make([]byte, int(lengthPrefix[0])+48)
	if _, err := io.ReadFull(r, frame); err != nil {
		return Greeting{}, err
	}

	var out Greeting
	protocolEnd := int(lengthPrefix[0])
	if string(frame[:protocolEnd]) != protocolName {
		return Greeting{}, fmt.Errorf("unexpected protocol string %q", string(frame[:protocolEnd]))
	}
	copy(out.Flags[:], frame[protocolEnd:protocolEnd+8])
	copy(out.InfoHash[:], frame[protocolEnd+8:protocolEnd+28])
	copy(out.PeerID[:], frame[protocolEnd+28:])
	return out, nil
}
