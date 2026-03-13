package engine

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"time"

	"github.com/mac/bt-refractor/internal/discovery"
	"github.com/mac/bt-refractor/internal/peerwire"
)

type peerSession struct {
	endpoint discovery.Endpoint
	conn     net.Conn
	bitmap   peerwire.Bitmap
	choked   bool
	settings Settings
}

func establishSession(ctx context.Context, endpoint discovery.Endpoint, infoHash, peerID [20]byte, settings Settings) (*peerSession, error) {
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

	if err := session.writeGreeting(peerwire.NewGreeting(infoHash, peerID)); err != nil {
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
	if bitfield.KeepAlive || bitfield.Kind != peerwire.KindBitfield {
		conn.Close()
		return nil, fmt.Errorf("peer %s did not send an initial bitfield", endpoint)
	}
	session.bitmap = append(peerwire.Bitmap(nil), bitfield.Payload...)

	if err := session.writePacket(peerwire.InterestedPacket()); err != nil {
		conn.Close()
		return nil, err
	}

	return session, nil
}

func (s *peerSession) Close() error {
	return s.conn.Close()
}

func (s *peerSession) Availability() peerwire.Bitmap {
	return s.bitmap
}

func (s *peerSession) SignalHave(index int) error {
	return s.writePacket(peerwire.HavePacket(index))
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
				if err := s.writePacket(peerwire.RequestPacket(pieceIndex, requested, blockLength)); err != nil {
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
		case peerwire.KindChoke:
			s.choked = true
		case peerwire.KindUnchoke:
			s.choked = false
		case peerwire.KindHave:
			index, err := peerwire.ParseHave(packet)
			if err != nil {
				return nil, err
			}
			s.bitmap.Mark(index)
		case peerwire.KindBitfield:
			s.bitmap = append(peerwire.Bitmap(nil), packet.Payload...)
		case peerwire.KindPiece:
			wrote, err := peerwire.CopyBlock(packet, pieceIndex, buffer)
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

func (s *peerSession) writeGreeting(greeting peerwire.Greeting) error {
	return s.writeRaw(greeting.Encode())
}

func (s *peerSession) readGreeting() (peerwire.Greeting, error) {
	if err := s.conn.SetDeadline(time.Now().Add(s.settings.IOTimeout)); err != nil {
		return peerwire.Greeting{}, err
	}
	defer s.conn.SetDeadline(time.Time{})
	return peerwire.ReadGreeting(s.conn)
}

func (s *peerSession) writePacket(packet peerwire.Packet) error {
	return s.writeRaw(packet.Encode())
}

func (s *peerSession) readPacket() (peerwire.Packet, error) {
	if err := s.conn.SetDeadline(time.Now().Add(s.settings.IOTimeout)); err != nil {
		return peerwire.Packet{}, err
	}
	defer s.conn.SetDeadline(time.Time{})
	return peerwire.ReadPacket(s.conn)
}

func (s *peerSession) writeRaw(payload []byte) error {
	if err := s.conn.SetDeadline(time.Now().Add(s.settings.IOTimeout)); err != nil {
		return err
	}
	defer s.conn.SetDeadline(time.Time{})
	_, err := s.conn.Write(payload)
	return err
}
