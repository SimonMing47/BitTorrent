package swarm

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"time"

	"github.com/mac/bt-refractor/internal/tracker"
	"github.com/mac/bt-refractor/internal/wire"
)

type peerSession struct {
	endpoint tracker.Endpoint
	conn     net.Conn
	bitmap   wire.Bitmap
	choked   bool
	settings Settings
}

func establishSession(ctx context.Context, endpoint tracker.Endpoint, infoHash, peerID [20]byte, settings Settings) (*peerSession, error) {
	dialer := &net.Dialer{Timeout: settings.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", endpoint.String())
	if err != nil {
		return nil, err
	}

	session := &peerSession{
		endpoint: endpoint,
		conn:     conn,
		choked:   true,
		settings: settings,
	}

	if err := session.writeGreeting(wire.NewGreeting(infoHash, peerID)); err != nil {
		conn.Close()
		return nil, err
	}

	greeting, err := session.readGreeting()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !bytes.Equal(greeting.InfoHash[:], infoHash[:]) {
		conn.Close()
		return nil, fmt.Errorf("peer %s replied with mismatched info hash", endpoint)
	}

	bitfield, err := session.readPacket()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if bitfield.KeepAlive || bitfield.Kind != wire.KindBitfield {
		conn.Close()
		return nil, fmt.Errorf("peer %s did not send an initial bitfield", endpoint)
	}
	session.bitmap = append(wire.Bitmap(nil), bitfield.Payload...)

	if err := session.writePacket(wire.InterestedPacket()); err != nil {
		conn.Close()
		return nil, err
	}

	return session, nil
}

func (s *peerSession) Close() error {
	return s.conn.Close()
}

func (s *peerSession) Availability() wire.Bitmap {
	return s.bitmap
}

func (s *peerSession) SignalHave(index int) error {
	return s.writePacket(wire.HavePacket(index))
}

func (s *peerSession) FetchPiece(ctx context.Context, pieceIndex, pieceLength int) ([]byte, error) {
	buffer := make([]byte, pieceLength)
	requested := 0
	received := 0
	inflight := 0

	for received < pieceLength {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if !s.choked {
			for inflight < s.settings.PipelineDepth && requested < pieceLength {
				blockLength := s.settings.BlockSize
				if pieceLength-requested < blockLength {
					blockLength = pieceLength - requested
				}
				if err := s.writePacket(wire.RequestPacket(pieceIndex, requested, blockLength)); err != nil {
					return nil, err
				}
				requested += blockLength
				inflight++
			}
		}

		packet, err := s.readPacket()
		if err != nil {
			return nil, err
		}
		if packet.KeepAlive {
			continue
		}

		switch packet.Kind {
		case wire.KindChoke:
			s.choked = true
		case wire.KindUnchoke:
			s.choked = false
		case wire.KindHave:
			index, err := wire.ParseHave(packet)
			if err != nil {
				return nil, err
			}
			s.bitmap.Mark(index)
		case wire.KindBitfield:
			s.bitmap = append(wire.Bitmap(nil), packet.Payload...)
		case wire.KindPiece:
			wrote, err := wire.CopyBlock(packet, pieceIndex, buffer)
			if err != nil {
				return nil, err
			}
			received += wrote
			if inflight > 0 {
				inflight--
			}
		}
	}

	return buffer, nil
}

func (s *peerSession) writeGreeting(greeting wire.Greeting) error {
	return s.writeRaw(greeting.Encode())
}

func (s *peerSession) readGreeting() (wire.Greeting, error) {
	if err := s.conn.SetDeadline(time.Now().Add(s.settings.IOTimeout)); err != nil {
		return wire.Greeting{}, err
	}
	defer s.conn.SetDeadline(time.Time{})
	return wire.ReadGreeting(s.conn)
}

func (s *peerSession) writePacket(packet wire.Packet) error {
	return s.writeRaw(packet.Encode())
}

func (s *peerSession) readPacket() (wire.Packet, error) {
	if err := s.conn.SetDeadline(time.Now().Add(s.settings.IOTimeout)); err != nil {
		return wire.Packet{}, err
	}
	defer s.conn.SetDeadline(time.Time{})
	return wire.ReadPacket(s.conn)
}

func (s *peerSession) writeRaw(payload []byte) error {
	if err := s.conn.SetDeadline(time.Now().Add(s.settings.IOTimeout)); err != nil {
		return err
	}
	defer s.conn.SetDeadline(time.Time{})
	_, err := s.conn.Write(payload)
	return err
}
