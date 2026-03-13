package engine

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/mac/bt-refractor/internal/discovery"
	"github.com/mac/bt-refractor/internal/manifest"
	"github.com/mac/bt-refractor/internal/peerwire"
)

const (
	defaultBlockSize     = 16 * 1024
	defaultPipelineDepth = 8
)

// Settings controls peer and network behavior.
type Settings struct {
	DialTimeout   time.Duration
	IOTimeout     time.Duration
	BlockSize     int
	PipelineDepth int
}

// Manager coordinates piece downloads across peers.
type Manager struct {
	meta     manifest.Manifest
	peers    []discovery.Endpoint
	peerID   [20]byte
	logger   *log.Logger
	settings Settings
}

// New constructs a manager for a torrent session.
func New(meta manifest.Manifest, peers []discovery.Endpoint, peerID [20]byte, logger *log.Logger, settings Settings) *Manager {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if settings.DialTimeout <= 0 {
		settings.DialTimeout = 3 * time.Second
	}
	if settings.IOTimeout <= 0 {
		settings.IOTimeout = 30 * time.Second
	}
	if settings.BlockSize <= 0 {
		settings.BlockSize = defaultBlockSize
	}
	if settings.PipelineDepth <= 0 {
		settings.PipelineDepth = defaultPipelineDepth
	}

	return &Manager{
		meta:     meta,
		peers:    peers,
		peerID:   peerID,
		logger:   logger,
		settings: settings,
	}
}

// Save downloads the torrent payload directly into the target file.
func (m *Manager) Save(ctx context.Context, targetPath string) error {
	if len(m.peers) == 0 {
		return fmt.Errorf("tracker returned no peers")
	}

	store, err := openFileStore(targetPath, m.meta.TotalLength)
	if err != nil {
		return err
	}
	defer store.Close()

	book := newCatalog(m.meta)
	var wg sync.WaitGroup

	for _, peer := range m.peers {
		wg.Add(1)
		go func(endpoint discovery.Endpoint) {
			defer wg.Done()
			m.runPeer(ctx, endpoint, book, store)
		}(peer)
	}

	wg.Wait()

	if store.err() != nil {
		return store.err()
	}
	if book.Completed() != m.meta.PieceCount() {
		return fmt.Errorf("download incomplete: %d of %d pieces verified", book.Completed(), m.meta.PieceCount())
	}
	return nil
}

func (m *Manager) runPeer(ctx context.Context, endpoint discovery.Endpoint, book *catalog, store *fileStore) {
	session, err := establishSession(ctx, endpoint, m.meta.InfoHash, m.peerID, m.settings)
	if err != nil {
		m.logger.Printf("peer %s unavailable: %v", endpoint, err)
		return
	}
	defer session.Close()

	m.logger.Printf("peer %s connected", endpoint)

	for {
		if ctx.Err() != nil {
			return
		}

		lease, ok, done := book.TryLease(session.Availability(), m.meta)
		if done {
			return
		}
		if !ok {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		block, err := session.FetchPiece(ctx, lease.Index, lease.Length)
		if err != nil {
			book.Release(lease.Index)
			m.logger.Printf("peer %s dropped piece %d: %v", endpoint, lease.Index, err)
			return
		}

		if digest := sha1.Sum(block); digest != lease.Digest {
			book.Release(lease.Index)
			m.logger.Printf("peer %s failed hash check for piece %d", endpoint, lease.Index)
			continue
		}

		if err := store.WriteAt(lease.Offset, block); err != nil {
			book.Release(lease.Index)
			m.logger.Printf("piece %d write failed: %v", lease.Index, err)
			return
		}

		doneCount, total := book.MarkDone(lease.Index)
		_ = session.SignalHave(lease.Index)
		m.logger.Printf("piece %d verified (%d/%d)", lease.Index, doneCount, total)
	}
}

type pieceLease struct {
	Index  int
	Offset int64
	Length int
	Digest [20]byte
}

type pieceState uint8

const (
	stateWaiting pieceState = iota
	stateLeased
	stateDone
)

type catalog struct {
	mu        sync.Mutex
	states    []pieceState
	completed int
}

func newCatalog(meta manifest.Manifest) *catalog {
	return &catalog{states: make([]pieceState, meta.PieceCount())}
}

func (c *catalog) TryLease(bitmap peerwire.Bitmap, meta manifest.Manifest) (pieceLease, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.completed == len(c.states) {
		return pieceLease{}, false, true
	}

	for index, state := range c.states {
		if state != stateWaiting || !bitmap.Contains(index) {
			continue
		}
		offset, length, err := meta.PieceSpan(index)
		if err != nil {
			return pieceLease{}, false, false
		}
		c.states[index] = stateLeased
		return pieceLease{
			Index:  index,
			Offset: offset,
			Length: length,
			Digest: meta.PieceDigests[index],
		}, true, false
	}
	return pieceLease{}, false, false
}

func (c *catalog) Release(index int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= 0 && index < len(c.states) && c.states[index] == stateLeased {
		c.states[index] = stateWaiting
	}
}

func (c *catalog) MarkDone(index int) (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= 0 && index < len(c.states) && c.states[index] != stateDone {
		c.states[index] = stateDone
		c.completed++
	}
	return c.completed, len(c.states)
}

func (c *catalog) Completed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.completed
}

type fileStore struct {
	mu       sync.Mutex
	file     *os.File
	writeErr error
}

func openFileStore(path string, size int64) (*fileStore, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := file.Truncate(size); err != nil {
		file.Close()
		return nil, err
	}
	return &fileStore{file: file}, nil
}

func (s *fileStore) WriteAt(offset int64, block []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writeErr != nil {
		return s.writeErr
	}
	_, err := s.file.WriteAt(block, offset)
	if err != nil {
		s.writeErr = err
	}
	return err
}

func (s *fileStore) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeErr
}

func (s *fileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}
