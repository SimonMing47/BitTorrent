# Comparison With `veggiedefender/torrent-client`

This repository was intentionally rewritten from scratch and does not mirror the original repository's package layout, file naming, data model naming, or download assembly approach.

## Functional Scope

The supported user-facing capability remains aligned with the original baseline:

- `.torrent` input
- single-file torrents
- tracker-based peer discovery
- BitTorrent peer handshake and piece download flow
- piece hash verification
- file output to disk

This repository additionally supports HTTPS tracker requests with:

- an optional PEM certificate file for tracker trust
- an optional skip-verify mode for tracker TLS validation

## Structural Differences

Reference repository layout:

- `torrentfile`
- `peers`
- `handshake`
- `message`
- `client`
- `p2p`

This repository layout:

- `internal/manifest`
- `internal/discovery`
- `internal/peerwire`
- `internal/engine`
- `cmd/riptide`

The naming was chosen deliberately to avoid following the reference repository's folder and file vocabulary.

## Runtime Design Differences

Reference repository behavior:

- parses torrent metadata into a `TorrentFile`
- requests peers from the tracker
- downloads the full payload into memory
- writes the completed buffer to disk at the end

This repository behavior:

- parses torrent metadata into a `Manifest`
- uses a `discovery.HTTPClient` with explicit dial options
- leases pieces through an internal catalog in the download engine
- verifies each piece and writes it directly to the destination file at its final offset

The direct-to-disk write path is a meaningful architectural difference from the original in-memory assembly model.

## Wire Layer Differences

Reference repository splits peer protocol responsibilities into separate packages and files for:

- handshake
- message
- bitfield
- client

This repository groups that protocol surface into `internal/peerwire` and `internal/engine`, with file naming such as:

- `hello.go`
- `frames.go`
- `availability.go`
- `peerlink.go`
- `downloader.go`

That means both the file map and the abstraction boundaries differ from the reference project.

## Tracker/TLS Differences

The original repository only uses a plain HTTP client for tracker requests.

This repository adds:

- `discovery.Options`
- custom HTTP transport construction
- plain TCP dialing when no tracker certificate options are supplied
- TLS dialing when a certificate path or skip-verify mode is supplied

In other words, HTTPS tracker support and certificate-aware connection setup are unique to this rewrite.

## Test Strategy Differences

Reference repository tests focus on small protocol helpers and parsing units.

This repository includes:

- unit tests for the local bencode codec
- manifest decoding tests
- tracker HTTP and HTTPS tests
- engine scheduling tests
- an end-to-end integration test with a fake tracker and fake peer

## Naming Differences

The following names were intentionally avoided:

- `torrentfile`
- `p2p`
- `client`
- `message`
- `handshake`
- `peers`

They were replaced with a different naming system centered around:

- `manifest`
- `discovery`
- `peerwire`
- `engine`
- `riptide`
