package manifest

import (
	"crypto/sha1"
	"fmt"
	"os"

	"github.com/mac/bt-refractor/internal/bencode"
)

// Manifest holds the supported subset of torrent metadata.
type Manifest struct {
	Announce            string
	Name                string
	TotalLength         int64
	StandardPieceLength int
	PieceDigests        [][20]byte
	InfoHash            [20]byte
}

// Load reads and decodes a .torrent file.
func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	return Parse(data)
}

// Parse turns raw .torrent bytes into a Manifest.
func Parse(data []byte) (Manifest, error) {
	rootValue, err := bencode.Parse(data)
	if err != nil {
		return Manifest{}, err
	}

	root, ok := rootValue.(map[string]any)
	if !ok {
		return Manifest{}, fmt.Errorf("torrent root must be a dictionary")
	}

	announce, err := getString(root, "announce")
	if err != nil {
		return Manifest{}, err
	}

	infoValue, ok := root["info"]
	if !ok {
		return Manifest{}, fmt.Errorf("missing info dictionary")
	}
	info, ok := infoValue.(map[string]any)
	if !ok {
		return Manifest{}, fmt.Errorf("info must be a dictionary")
	}

	if _, exists := info["files"]; exists {
		return Manifest{}, fmt.Errorf("multi-file torrents are not supported")
	}

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		return Manifest{}, err
	}
	infoHash := sha1.Sum(infoBytes)

	name, err := getString(info, "name")
	if err != nil {
		return Manifest{}, err
	}

	pieceLength, err := getInt(info, "piece length")
	if err != nil {
		return Manifest{}, err
	}
	if pieceLength <= 0 {
		return Manifest{}, fmt.Errorf("piece length must be positive")
	}

	totalLength, err := getInt(info, "length")
	if err != nil {
		return Manifest{}, err
	}
	if totalLength < 0 {
		return Manifest{}, fmt.Errorf("length must be non-negative")
	}

	piecesBlob, err := getBytes(info, "pieces")
	if err != nil {
		return Manifest{}, err
	}
	digests, err := splitDigests(piecesBlob)
	if err != nil {
		return Manifest{}, err
	}

	return Manifest{
		Announce:            announce,
		Name:                name,
		TotalLength:         totalLength,
		StandardPieceLength: int(pieceLength),
		PieceDigests:        digests,
		InfoHash:            infoHash,
	}, nil
}

// PieceCount reports the number of pieces in the torrent.
func (m Manifest) PieceCount() int {
	return len(m.PieceDigests)
}

// PieceSpan returns the absolute offset and piece length for a piece index.
func (m Manifest) PieceSpan(index int) (int64, int, error) {
	if index < 0 || index >= len(m.PieceDigests) {
		return 0, 0, fmt.Errorf("piece index %d out of bounds", index)
	}

	offset := int64(index * m.StandardPieceLength)
	remaining := m.TotalLength - offset
	if remaining <= 0 {
		return 0, 0, fmt.Errorf("piece index %d exceeds torrent length", index)
	}
	length := m.StandardPieceLength
	if remaining < int64(length) {
		length = int(remaining)
	}
	return offset, length, nil
}

func splitDigests(blob []byte) ([][20]byte, error) {
	const digestLength = 20
	if len(blob)%digestLength != 0 {
		return nil, fmt.Errorf("pieces blob has invalid length %d", len(blob))
	}
	count := len(blob) / digestLength
	digests := make([][20]byte, count)
	for idx := 0; idx < count; idx++ {
		copy(digests[idx][:], blob[idx*digestLength:(idx+1)*digestLength])
	}
	return digests, nil
}

func getString(dict map[string]any, key string) (string, error) {
	value, err := getBytes(dict, key)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func getBytes(dict map[string]any, key string) ([]byte, error) {
	value, ok := dict[key]
	if !ok {
		return nil, fmt.Errorf("missing key %q", key)
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil, fmt.Errorf("key %q must be a byte string", key)
	}
	return bytes, nil
}

func getInt(dict map[string]any, key string) (int64, error) {
	value, ok := dict[key]
	if !ok {
		return 0, fmt.Errorf("missing key %q", key)
	}
	number, ok := value.(int64)
	if !ok {
		return 0, fmt.Errorf("key %q must be an integer", key)
	}
	return number, nil
}
