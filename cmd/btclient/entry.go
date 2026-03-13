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

	torrentPath := fs.String("i", "", "种子文件路径")
	outputDir := fs.String("o", "", "下载输出目录路径")
	tlsPath := fs.String("tls-path", "", "tracker 安全模式证书路径")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *torrentPath == "" || *outputDir == "" {
		fmt.Fprintln(stderr, "用法: btclient -i input.torrent -o output.dir [-tls-path cert.pem]")
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
		fmt.Fprintf(stderr, "下载失败: %v\n", err)
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
	logger.Printf("tracker 返回 %d 个 peer", len(reply.Peers))

	manager := engine.New(meta, reply.Peers, peerID, logger, engine.Settings{})
	return manager.Save(ctx, outputPath)
}

func buildOutputPath(outputDir, torrentName string) (string, error) {
	fileName := filepath.Base(torrentName)
	if fileName == "." || fileName == string(filepath.Separator) || fileName == "" {
		return "", fmt.Errorf("种子中的文件名无效: %q", torrentName)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("创建输出目录失败: %w", err)
	}
	return filepath.Join(outputDir, fileName), nil
}

func generatePeerID() ([20]byte, error) {
	var peerID [20]byte
	copy(peerID[:], []byte("-BR0001-"))
	_, err := rand.Read(peerID[8:])
	return peerID, err
}
