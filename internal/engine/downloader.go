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
	defaultPipelineDepth = 64
	defaultAuditPieces   = 32
	defaultRepairRounds  = 3
	progressLogInterval  = 64
)

// Settings 控制 peer 连接和下载行为的关键参数。
type Settings struct {
	DialTimeout   time.Duration
	IOTimeout     time.Duration
	BlockSize     int
	PipelineDepth int
	VerifyPieces  bool
	AuditPieces   int
	RepairRounds  int
}

// Manager 负责在多个 peer 之间调度 piece 下载。
type Manager struct {
	meta     manifest.Manifest
	peers    []discovery.Endpoint
	peerID   [20]byte
	logger   *log.Logger
	settings Settings
}

// New 创建一个 torrent 下载会话的调度器。
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
	if settings.AuditPieces < 0 {
		settings.AuditPieces = 0
	}
	if !settings.VerifyPieces && settings.AuditPieces == 0 {
		settings.AuditPieces = defaultAuditPieces
	}
	if settings.RepairRounds <= 0 {
		settings.RepairRounds = defaultRepairRounds
	}

	return &Manager{
		meta:     meta,
		peers:    peers,
		peerID:   peerID,
		logger:   logger,
		settings: settings,
	}
}

// Save 将整个 torrent 直接下载到目标文件，而不是先整体放入内存。
func (m *Manager) Save(ctx context.Context, targetPath string) error {
	if len(m.peers) == 0 {
		return fmt.Errorf("tracker returned no peers")
	}

	store, err := openFileStore(targetPath, m.meta.TotalLength)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := m.downloadWithCatalog(ctx, newCatalog(m.meta), store, m.settings); err != nil {
		return err
	}
	if err := store.Sync(); err != nil {
		return err
	}
	if !m.settings.VerifyPieces && m.settings.AuditPieces > 0 {
		checked, failed, err := auditDownloadedPieces(targetPath, m.meta, m.settings.AuditPieces)
		if err != nil {
			return err
		}
		if failed >= 0 {
			m.logger.Printf("post-download audit detected corruption at piece %d, escalating to full verification", failed)
			if err := m.repairCorruptPieces(ctx, targetPath); err != nil {
				return err
			}
		} else {
			m.logger.Printf("post-download audit passed (%d pieces)", checked)
		}
	}
	return nil
}

func (m *Manager) downloadWithCatalog(ctx context.Context, book *catalog, store *fileStore, settings Settings) error {
	var wg sync.WaitGroup

	for _, peer := range m.peers {
		wg.Add(1)
		go func(endpoint discovery.Endpoint) {
			defer wg.Done()
			m.runPeer(ctx, endpoint, book, store, settings)
		}(peer)
	}

	wg.Wait()

	if store.err() != nil {
		return store.err()
	}
	if book.Completed() != m.meta.PieceCount() {
		return fmt.Errorf("download incomplete: %d of %d pieces stored", book.Completed(), m.meta.PieceCount())
	}
	return nil
}

func (m *Manager) repairCorruptPieces(ctx context.Context, targetPath string) error {
	corrupt, err := collectCorruptPieces(targetPath, m.meta, allPieceIndexes(m.meta.PieceCount()))
	if err != nil {
		return err
	}
	if len(corrupt) == 0 {
		return fmt.Errorf("post-download audit escalated, but full verification found no corrupt pieces")
	}

	m.logger.Printf("full verification found %d corrupt pieces", len(corrupt))

	strictSettings := m.settings
	strictSettings.VerifyPieces = true
	strictSettings.AuditPieces = 0

	for round := 1; round <= strictSettings.RepairRounds && len(corrupt) > 0; round++ {
		store, err := openExistingFileStore(targetPath)
		if err != nil {
			return err
		}

		m.logger.Printf("repair round %d started for %d pieces", round, len(corrupt))
		downloadErr := m.downloadWithCatalog(ctx, newCatalogWithPending(m.meta, corrupt), store, strictSettings)
		syncErr := store.Sync()
		closeErr := store.Close()

		if downloadErr != nil {
			return downloadErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}

		corrupt, err = collectCorruptPieces(targetPath, m.meta, corrupt)
		if err != nil {
			return err
		}
	}

	if len(corrupt) > 0 {
		return fmt.Errorf("repair failed for pieces %v", corrupt)
	}

	m.logger.Printf("repair completed successfully")
	return nil
}

func (m *Manager) runPeer(ctx context.Context, endpoint discovery.Endpoint, book *catalog, store *fileStore, settings Settings) {
	session, err := establishSession(ctx, endpoint, m.meta.InfoHash, m.peerID, settings)
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
			time.Sleep(10 * time.Millisecond)
			continue
		}

		block, err := session.FetchPiece(ctx, lease.Index, lease.Length)
		if err != nil {
			book.Release(lease.Index)
			m.logger.Printf("peer %s dropped piece %d: %v", endpoint, lease.Index, err)
			return
		}

		if settings.VerifyPieces {
			if digest := sha1.Sum(block); digest != lease.Digest {
				book.Release(lease.Index)
				m.logger.Printf("peer %s failed hash check for piece %d", endpoint, lease.Index)
				return
			}
		}

		if err := store.WriteAt(lease.Offset, block); err != nil {
			book.Release(lease.Index)
			m.logger.Printf("piece %d write failed: %v", lease.Index, err)
			return
		}

		doneCount, total := book.MarkDone(lease.Index)
		_ = session.SignalHave(lease.Index)
		if doneCount == total || doneCount%progressLogInterval == 0 {
			if settings.VerifyPieces {
				m.logger.Printf("completed %d/%d pieces (verified)", doneCount, total)
			} else {
				m.logger.Printf("completed %d/%d pieces", doneCount, total)
			}
		}
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

func newCatalogWithPending(meta manifest.Manifest, pending []int) *catalog {
	states := make([]pieceState, meta.PieceCount())
	for index := range states {
		states[index] = stateDone
	}

	completed := len(states)
	for _, index := range pending {
		if index < 0 || index >= len(states) {
			continue
		}
		if states[index] != stateWaiting {
			states[index] = stateWaiting
			completed--
		}
	}

	return &catalog{
		states:    states,
		completed: completed,
	}
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

func openExistingFileStore(path string) (*fileStore, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
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

func (s *fileStore) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writeErr != nil {
		return s.writeErr
	}
	return s.file.Sync()
}

func (s *fileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}

func auditDownloadedPieces(path string, meta manifest.Manifest, count int) (int, int, error) {
	indexes := selectAuditPieces(meta.PieceCount(), count)
	corrupt, err := collectCorruptPieces(path, meta, indexes)
	if err != nil {
		return 0, -1, err
	}
	if len(corrupt) > 0 {
		return len(indexes), corrupt[0], nil
	}
	return len(indexes), -1, nil
}

func collectCorruptPieces(path string, meta manifest.Manifest, indexes []int) ([]int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var corrupt []int
	for _, index := range indexes {
		offset, length, err := meta.PieceSpan(index)
		if err != nil {
			return nil, err
		}
		block := make([]byte, length)
		if _, err := file.ReadAt(block, offset); err != nil {
			return nil, fmt.Errorf("read piece %d for verification: %w", index, err)
		}
		if digest := sha1.Sum(block); digest != meta.PieceDigests[index] {
			corrupt = append(corrupt, index)
		}
	}
	return corrupt, nil
}

func allPieceIndexes(total int) []int {
	if total <= 0 {
		return nil
	}
	indexes := make([]int, total)
	for index := range indexes {
		indexes[index] = index
	}
	return indexes
}

func selectAuditPieces(total, count int) []int {
	if total <= 0 || count <= 0 {
		return nil
	}
	if count == 1 {
		return []int{0}
	}
	if count >= total {
		indexes := make([]int, total)
		for index := range indexes {
			indexes[index] = index
		}
		return indexes
	}

	indexes := make([]int, 0, count)
	last := -1
	for slot := 0; slot < count; slot++ {
		index := slot * (total - 1) / (count - 1)
		if index == last {
			continue
		}
		indexes = append(indexes, index)
		last = index
	}
	return indexes
}
