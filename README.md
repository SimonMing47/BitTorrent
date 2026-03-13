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

- `cmd/bt`: CLI entrypoint
- `internal/bencode`: minimal bencode parser/encoder
- `internal/metainfo`: `.torrent` decoding and info-hash calculation
- `internal/tracker`: HTTP announce client
- `internal/wire`: BitTorrent handshake and peer message codec
- `internal/swarm`: peer sessions, piece scheduling, and file assembly

## Usage

```bash
go run ./cmd/bt --torrent path/to/file.torrent --out path/to/output.bin
```

For HTTPS trackers:

```bash
go run ./cmd/bt \
  --torrent path/to/file.torrent \
  --out path/to/output.bin \
  --tracker-cert path/to/tracker.pem
```

To explicitly skip TLS verification for the tracker:

```bash
go run ./cmd/bt \
  --torrent path/to/file.torrent \
  --out path/to/output.bin \
  --tracker-skip-verify
```

Positional arguments are also supported:

```bash
go run ./cmd/bt path/to/file.torrent path/to/output.bin
```

## Verification

```bash
go test -count=1 ./...
go build ./cmd/bt
```
