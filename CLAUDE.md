# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

This project uses [Task](https://taskfile.dev/) as its task runner (`Taskfile.yml`).

```sh
task build        # Format + build optimized static binary (CGO_ENABLED=0, -pgo=auto)
task build-debug  # Format + build race-detector binary with CGO enabled
task test         # go test ./...
task lint         # Format + golangci-lint run --timeout 5m
task fmt          # gci write . && gofumpt -l -w . && betteralign -apply ./...
task update       # go get -u && go mod tidy
```

Run a specific test:
```sh
go test -run TestName ./...
```

For dependency source inspection:
```sh
go mod download -json MODULE   # get Dir path
go doc foo.bar                 # read package/type/func docs
```

Prefer `go run .` over `go build` to avoid leaving binary artifacts.

## Architecture

Single-package `main` CLI tool that performs DNS zone transfers (AXFR) or parses local RFC 1035 zone files, then emits a Unix-style `hosts` file to stdout.

**Data flow:**

```
parseFlags()  →  zones + server + cidrList
                       │
        ┌──────────────┴──────────────┐
        │  worker goroutines (per zone) │
        │  processRemoteZone()          │  ← zoneTransfer() via miekg/dns AXFR
        │  processLocalZone()           │  ← zoneParser() via miekg/dns ZoneParser
        │         ↓                     │
        │  processRecords()             │  ← spawns goroutines per RR (A/AAAA/CNAME)
        │  processHost() → hostChan ←──┘
        │
        ↓ (buffered channel, size 2048)
   monitor goroutine: writeHostEntries()
        ↓
   HostMap (map[netip.Addr]map[string]struct{})
        ↓
   displayHostEntries() → stdout
```

**Key design decisions:**
- **Lock-free map writes**: a single monitor goroutine owns `HostMap` and `keys []netip.Addr`; worker goroutines only send to `hostChan`. No mutex needed on the shared map.
- **AXFR concurrency**: limited by a semaphore channel (`semAXFR`) sized by `-max_transfers` (default 10, matching BIND9's `transfers-out`).
- **CNAME dedup**: `lookup.go` uses `golang.org/x/sync/singleflight` to collapse concurrent DNS lookups for the same hostname into one network call.
- **CIDR filtering**: `ranger.go` uses `monoidic/cidranger/v2` (Patricia trie) for efficient subnet membership tests. Filtering happens per-RR before sending to channel.
- **Memory/CPU limits**: `automemlimit` + `automaxprocs` are set at startup to respect cgroup constraints.

**File responsibilities:**

| File | Role |
|------|------|
| `main.go` | Entry point, goroutine orchestration, pprof hooks |
| `options.go` | All `flag` definitions and `parseFlags()` |
| `axfr.go` | `zoneTransfer()` (AXFR), `zoneParser()` (local files), `processRecords()`, `processLocalZone()`, `processRemoteZone()` |
| `hosts.go` | `HostEntry`/`HostMap` types, `processHost()`, `writeHostEntries()` |
| `output.go` | `displayHostEntries()` — sorts by IP then hostname, prints hosts file |
| `lookup.go` | `lookup()` + `lookupFunc()` — singleflight DNS resolver for CNAME resolution |
| `ranger.go` | `rangerInit()` — CIDR whitelist Patricia trie setup |
| `rlimit.go` / `rlimit_unix.go` | Platform-specific `setNofile()` to raise open-file limits |

## Linting

`golangci-lint` v2 with `default: all` linters. Key disabled linters: `cyclop`, `mnd`, `lll`, `varnamelen`, `wrapcheck`, `exhaustruct`. Test files (`*_test.go`) are excluded from most checks. Formatter chain: `gci` → `gofmt` → `gofumpt` → `goimports`. Always run `task fmt` before committing.

## Build notes

- `CGO_ENABLED=0` by default (static binary); set to `1` only for `build-debug` (enables `-race`).
- Version metadata is injected at link time: `main.GitTag`, `main.GitCommit`, `main.GitDirty`, `main.BuildTime`.
- Releases are handled by `goreleaser` (`goreleaser.yml`); use `task release` to cut a release.
