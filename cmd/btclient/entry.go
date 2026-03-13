package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/mac/bt-refractor/internal/discovery"
	"github.com/mac/bt-refractor/internal/engine"
	"github.com/mac/bt-refractor/internal/manifest"
)

const defaultAnnouncePort = 6881

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("btclient", flag.ContinueOnError)
	fs.SetOutput(stderr)

	torrentPath := fs.String("i", "", "path to the input torrent file")
	outputDir := fs.String("o", "", "output root path")
	tlsPath := fs.String("tls-path", "", "PEM certificate path for secure tracker requests")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *torrentPath == "" || *outputDir == "" {
		fmt.Fprintln(stderr, "Usage: btclient -i INPUT.torrent -o OUTPUT_PATH [-tls-path CERT.pem]")
		return 2
	}

	if err := execute(
		context.Background(),
		*torrentPath,
		*outputDir,
		uint16(defaultAnnouncePort),
		discovery.Options{
			TLSPath: *tlsPath,
		},
		stdout,
	); err != nil {
		fmt.Fprintf(stderr, "download failed: %v\n", err)
		return 1
	}
	return 0
}

func execute(
	ctx context.Context,
	torrentPath, outputDir string,
	port uint16,
	trackerOptions discovery.Options,
	logWriter io.Writer,
) error {
	meta, err := manifest.Load(torrentPath)
	if err != nil {
		return err
	}

	outputPath, err := buildOutputPath(outputDir, meta.Name)
	if err != nil {
		return err
	}

	peerID, err := generatePeerID()
	if err != nil {
		return err
	}

	announce, err := discovery.NewWithOptions(trackerOptions)
	if err != nil {
		return err
	}
	reply, err := announce.Announce(ctx, meta.Announce, discovery.AnnounceRequest{
		InfoHash: meta.InfoHash,
		PeerID:   peerID,
		Port:     port,
		Left:     meta.TotalLength,
		Compact:  true,
	})
	if err != nil {
		return err
	}

	logger := log.New(logWriter, "", log.LstdFlags)
	logger.Printf("tracker returned %d peers", len(reply.Peers))

	manager := engine.New(meta, reply.Peers, peerID, logger, engine.Settings{
		VerifyPieces: envFlagEnabled("BTCLIENT_VERIFY_PIECES"),
	})
	return manager.Save(ctx, outputPath)
}

func buildOutputPath(outputDir, torrentName string) (string, error) {
	cleanName := filepath.Clean(torrentName)
	if torrentName == "" || cleanName == "." || cleanName == ".." || filepath.IsAbs(torrentName) || filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("invalid torrent name %q", torrentName)
	}
	if strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid torrent name %q", torrentName)
	}
	target := filepath.Join(outputDir, cleanName)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create output path: %w", err)
	}
	return target, nil
}

func generatePeerID() ([20]byte, error) {
	var peerID [20]byte
	copy(peerID[:], []byte("-BR0001-"))
	_, err := rand.Read(peerID[8:])
	return peerID, err
}

func envFlagEnabled(key string) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
