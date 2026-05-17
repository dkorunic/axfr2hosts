## Commands

Use `Taskfile.yml` for all project commands. Do not guess; these are the real ones:

- **Run app**: `go run .` — or `go run . zone @server` (see README)
- **Test**: `task test` — runs `go test ./...`
- **Run a single test**: `go test -run TestName ./...`
- **Lint**: `task lint` — runs `task fmt` + `golangci-lint run --timeout 5m`
- **Format**: `task fmt` — `gci` → `gofmt` → `gofumpt` → `betteralign`
- **Build**: `task build` — formats then builds static binary with injected version ldflags
- **Debug build** (race detector): `task build-debug` — sets `CGO_ENABLED=1`

**Always run `task fmt` before committing.** The formatter chain is the repo convention.

## Architecture

Single-package `main` CLI. DNS zone transfer (AXFR) or local RFC 1035 zone file parsing → buffered channel → lock-free `HostMap` → sorted stdout.

- `main.go:18` — constants: `mapSize=4096`, `subMapSize=8`, `hostChanSize=2048`, `maxTransfers=10`
- `main.go:89` — sole `HostMap` owner is the monitor goroutine (`writeHostEntries`); workers only send to `hostChan`. No mutex.
- `lookup.go` — CNAME resolution uses `golang.org/x/sync/singleflight` to collapse identical lookups.
- `ranger.go` — CIDR filtering via `monoidic/cidrernger/v2` Patricia trie.
- `rlimit*.go` — raises `RLIMIT_NOFILE` via `setNofile()`.

Full architecture in `CLAUDE.md`.

## Dependency inspection

- Source: `go mod download -json MODULE_PATH` → use returned `Dir`
- Docs: `go doc -all PACKAGE` or `go doc -all PACKAGE TYPE`

Prefer `go run .` over `go build` to avoid leaving binary artifacts.

## Constraints that differ from defaults

- **golangci-lint v2** with `default: all` linters. Disabled: `cyclop`, `mnd`, `lll`, `varnamelen`, `wrapcheck`, `exhaustruct`, etc. Test files excluded from most checks.
- **Go 1.26** — check `go.mod` for version before adding features.
- **Static binary by default** (`CGO_ENABLED=0`). Race detector requires `task build-debug`.
- **Version metadata** injected at link time: `main.GitTag`, `GitCommit`, `GitDirty`, `BuildTime`.
- **goreleaser** handles releases (`task release`). `goreleaser.yml` has platform guards (no Windows ARM/arm64).
