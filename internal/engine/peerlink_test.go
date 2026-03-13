package engine

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mac/bt-refractor/internal/discovery"
	"github.com/mac/bt-refractor/internal/peerwire"
)

func TestEstablishSession(t *testing.T) {
	infoHash := [20]byte{1, 2, 3, 4}
	peerID := [20]byte{5, 6, 7, 8}
	settings := Settings{
		DialTimeout:   time.Second,
		IOTimeout:     time.Second,
		BlockSize:     4,
		PipelineDepth: 2,
	}

	listener := mustListenLocal(t)
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveHandshakePeer(listener, infoHash, peerwire.Bitmap{0b1100_0000}, func(conn net.Conn) error {
			packet, err := peerwire.ReadPacket(conn)
			if err != nil {
				return err
			}
			if packet.Kind != peerwire.KindInterested {
				return errors.New("客户端没有发送 interested")
			}
			return nil
		})
	}()

	session, err := establishSession(context.Background(), endpointFromListener(listener), infoHash, peerID, settings)
	if err != nil {
		t.Fatalf("establishSession() error = %v", err)
	}
	defer session.Close()

	if !session.Availability().Contains(0) || !session.Availability().Contains(1) {
		t.Fatalf("unexpected availability bitmap: %08b", []byte(session.Availability()))
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("peer script error = %v", err)
	}
}

func TestEstablishSessionRejectsMismatchedInfoHash(t *testing.T) {
	infoHash := [20]byte{9, 9, 9}
	settings := Settings{
		DialTimeout: time.Second,
		IOTimeout:   time.Second,
	}

	listener := mustListenLocal(t)
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		wrongHash := infoHash
		wrongHash[0] = 1
		serverDone <- serveHandshakePeer(listener, wrongHash, peerwire.Bitmap{0b1000_0000}, nil)
	}()

	_, err := establishSession(context.Background(), endpointFromListener(listener), infoHash, [20]byte{1}, settings)
	if err == nil {
		t.Fatal("establishSession() error = nil, want mismatch failure")
	}
	if !strings.Contains(err.Error(), "mismatched info hash") {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("peer script error = %v", err)
	}
}

func TestPeerSessionFetchPieceAndSignalHave(t *testing.T) {
	infoHash := [20]byte{4, 3, 2, 1}
	peerID := [20]byte{8, 7, 6, 5}
	payload := []byte("hello world")
	settings := Settings{
		DialTimeout:   time.Second,
		IOTimeout:     time.Second,
		BlockSize:     4,
		PipelineDepth: 2,
	}

	listener := mustListenLocal(t)
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveHandshakePeer(listener, infoHash, peerwire.Bitmap{0b1000_0000}, func(conn net.Conn) error {
			packet, err := peerwire.ReadPacket(conn)
			if err != nil {
				return err
			}
			if packet.Kind != peerwire.KindInterested {
				return errors.New("客户端没有发送 interested")
			}

			if _, err := conn.Write(peerwire.Control(peerwire.KindUnchoke).Encode()); err != nil {
				return err
			}

			served := 0
			for served < len(payload) {
				request, err := peerwire.ReadPacket(conn)
				if err != nil {
					return err
				}
				if request.Kind != peerwire.KindRequest {
					return errors.New("客户端没有发送 request")
				}

				pieceIndex, begin, length, err := peerwire.ParseRequest(request)
				if err != nil {
					return err
				}
				if pieceIndex != 0 {
					return errors.New("请求了错误的 piece 编号")
				}

				block := append([]byte(nil), payload[begin:begin+length]...)
				if _, err := conn.Write(peerwire.PiecePacket(pieceIndex, begin, block).Encode()); err != nil {
					return err
				}
				served += length
			}

			have, err := peerwire.ReadPacket(conn)
			if err != nil {
				return err
			}
			index, err := peerwire.ParseHave(have)
			if err != nil {
				return err
			}
			if index != 3 {
				return errors.New("客户端发送了错误的 have 编号")
			}
			return nil
		})
	}()

	session, err := establishSession(context.Background(), endpointFromListener(listener), infoHash, peerID, settings)
	if err != nil {
		t.Fatalf("establishSession() error = %v", err)
	}
	defer session.Close()

	block, err := session.FetchPiece(context.Background(), 0, len(payload))
	if err != nil {
		t.Fatalf("FetchPiece() error = %v", err)
	}
	if !bytes.Equal(block, payload) {
		t.Fatalf("unexpected piece data: %q", block)
	}

	if err := session.SignalHave(3); err != nil {
		t.Fatalf("SignalHave() error = %v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("peer script error = %v", err)
	}
}

func mustListenLocal(t *testing.T) net.Listener {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	return listener
}

func endpointFromListener(listener net.Listener) discovery.Endpoint {
	addr := listener.Addr().(*net.TCPAddr)
	return discovery.Endpoint{
		Address: addr.IP,
		Port:    uint16(addr.Port),
	}
}

func serveHandshakePeer(listener net.Listener, infoHash [20]byte, bitmap peerwire.Bitmap, script func(net.Conn) error) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	greeting, err := peerwire.ReadGreeting(conn)
	if err != nil {
		return err
	}
	if _, err := conn.Write(peerwire.NewGreeting(infoHash, greeting.PeerID).Encode()); err != nil {
		return err
	}
	if _, err := conn.Write(peerwire.Packet{Kind: peerwire.KindBitfield, Payload: bitmap}.Encode()); err != nil {
		return err
	}
	if script != nil {
		return script(conn)
	}
	return nil
}
