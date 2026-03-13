# bt-refractor

An original BitTorrent client implementation in Go.

## Scope

Current support matches the referenced baseline feature set:

- `.torrent` files
- single-file torrents
- HTTP tracker announce
- HTTPS tracker announce with optional custom certificate and TLS verify skip
- compact peer list decoding
- peer handshake, bitfield, interested, request, piece, and have messages
- piece SHA-1 verification
- direct-to-disk download output

Current non-goals:

- magnet links
- UDP trackers
- multi-file torrents
- seeding/upload serving

## Layout

- `cmd/riptide`: CLI entrypoint
- `internal/bencode`: minimal bencode parser/encoder
- `internal/manifest`: `.torrent` decoding and info-hash calculation
- `internal/discovery`: tracker-facing peer discovery client
- `internal/peerwire`: BitTorrent handshake and peer message codec
- `internal/engine`: peer sessions, piece scheduling, and file assembly

See [docs/compare-with-veggiedefender.md](/Users/mac/projects/bt-refractor/docs/compare-with-veggiedefender.md) for a direct comparison with the reference repository.

## Usage

```bash
go run ./cmd/riptide --torrent path/to/file.torrent --out path/to/output.bin
```

For HTTPS trackers:

```bash
go run ./cmd/riptide \
  --torrent path/to/file.torrent \
  --out path/to/output.bin \
  --tracker-cert path/to/tracker.pem
```

To explicitly skip TLS verification for the tracker:

```bash
go run ./cmd/riptide \
  --torrent path/to/file.torrent \
  --out path/to/output.bin \
  --tracker-skip-verify
```

Positional arguments are also supported:

```bash
go run ./cmd/riptide path/to/file.torrent path/to/output.bin
```

## Verification

```bash
go test -count=1 ./...
go build ./cmd/riptide
```
