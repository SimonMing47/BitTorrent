package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/mac/bt-refractor/internal/metainfo"
	"github.com/mac/bt-refractor/internal/swarm"
	"github.com/mac/bt-refractor/internal/tracker"
)

const defaultAnnouncePort = 6881

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bt", flag.ContinueOnError)
	fs.SetOutput(stderr)

	torrentPath := fs.String("torrent", "", "path to the .torrent file")
	outputPath := fs.String("out", "", "path to the output file")
	port := fs.Uint("port", defaultAnnouncePort, "tracker port to announce")
	trackerCert := fs.String("tracker-cert", "", "path to a PEM certificate used for HTTPS tracker requests")
	trackerSkipVerify := fs.Bool("tracker-skip-verify", false, "skip TLS verification for HTTPS tracker requests")
	quiet := fs.Bool("quiet", false, "suppress progress logging")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *torrentPath == "" || *outputPath == "" {
		rest := fs.Args()
		if len(rest) == 2 {
			*torrentPath = rest[0]
			*outputPath = rest[1]
		}
	}

	if *torrentPath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "usage: bt -torrent input.torrent -out output.file")
		fmt.Fprintln(stderr, "   or: bt input.torrent output.file")
		return 2
	}

	if err := execute(
		context.Background(),
		*torrentPath,
		*outputPath,
		uint16(*port),
		tracker.Options{
			CertificatePath: *trackerCert,
			SkipTLSVerify:   *trackerSkipVerify,
		},
		*quiet,
		stdout,
	); err != nil {
		fmt.Fprintf(stderr, "download failed: %v\n", err)
		return 1
	}
	return 0
}

func execute(
	ctx context.Context,
	torrentPath, outputPath string,
	port uint16,
	trackerOptions tracker.Options,
	quiet bool,
	logWriter io.Writer,
) error {
	meta, err := metainfo.Load(torrentPath)
	if err != nil {
		return err
	}

	peerID, err := generatePeerID()
	if err != nil {
		return err
	}

	announce, err := tracker.NewWithOptions(trackerOptions)
	if err != nil {
		return err
	}
	reply, err := announce.Announce(ctx, meta.Announce, tracker.AnnounceRequest{
		InfoHash: meta.InfoHash,
		PeerID:   peerID,
		Port:     port,
		Left:     meta.TotalLength,
		Compact:  true,
	})
	if err != nil {
		return err
	}

	logger := log.New(io.Discard, "", log.LstdFlags)
	if !quiet {
		logger = log.New(logWriter, "", log.LstdFlags)
		logger.Printf("tracker returned %d peer(s)", len(reply.Peers))
	}

	manager := swarm.New(meta, reply.Peers, peerID, logger, swarm.Settings{})
	return manager.Save(ctx, outputPath)
}

func generatePeerID() ([20]byte, error) {
	var peerID [20]byte
	copy(peerID[:], []byte("-BR0001-"))
	_, err := rand.Read(peerID[8:])
	return peerID, err
}
