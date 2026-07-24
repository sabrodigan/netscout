# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

netscout is a single-binary Go web app that TCP-connect scans a local IPv4 network for live
hosts, stores every scan as an immutable MongoDB document, and diffs the two most recent scans
(or any two explicit scans). No `nmap`, no raw sockets, no root required.

## Commands

```bash
go build -o netscout .   # build (embeds web/ into the binary via //go:embed)
./netscout               # run; serves http://127.0.0.1:8092, needs MongoDB reachable
go vet ./...             # static checks
go test ./...            # no tests exist yet — this reports "no test files"
```

There is no lint config beyond `go vet`. `MONGO_URI` overrides the Mongo connection
(default `mongodb://localhost:27017`). The listen address `127.0.0.1:8092` is hardcoded in
`main.go`. Rebuild after editing anything under `web/` — assets are embedded at compile time,
not served from disk.

## Architecture

Flat `package main` across all `.go` files. Request flow: `main.go` wires a gorilla/mux router,
handlers in `handlers.go` call into the scanner and DB layers, and results serialize back as JSON.

- **`scanner.go`** — the core. `runScan` fans work across a fixed pool of 128 worker goroutines
  (jobs channel in, `Host` results channel out). A host counts as **up** if any probed port either
  accepts the connection *or* refuses it with a RST (`isConnRefused`) — a refusal still proves the
  host is alive, so a host silent on every port reads as absent. `localCIDR` auto-derives the
  primary non-loopback /24 when a scan request omits the CIDR. Pure-Go helpers (`hostsInCIDR`,
  `nextIP`, `ipLess`) handle IPv4 address math; IPv6 is explicitly rejected.
- **`db.go`** — package-level `client`/`db`/`scanCol` globals set by `InitDB()` (called once from
  `main`). Database `netscout`, collection `scans`, with a descending index on `finished_at` so
  "latest" lookups are cheap.
- **`diff.go`** — `diffScans(from, to)` keys hosts by IP into added / removed / changed buckets;
  `portDelta` computes per-host open-port deltas. Pure functions, no I/O.
- **`models.go`** — `Scan` (full doc, hosts embedded), `ScanSummary` (projection used by the
  history list — `hosts`/`ports` excluded), `Host`, `HostChange`, `ScanDiff`. Struct tags carry
  **both** `bson` (snake_case, for Mongo) and `json` (camelCase, for the API); keep them in sync.
- **`web/`** — static SPA (`index.html`, `app.js`, `style.css`) embedded and served at `/`.

## Data model invariant

Scans are **append-only** — every run is a new document; nothing is ever overwritten. History and
diffing depend on this. `findLatestScans(n)` returns newest-first, so `latestDiffHandler` passes
`scans[1]` (previous) as `from` and `scans[0]` (latest) as `to`.

## Routes

Registered in `main.go`. Note `/scans/{id}` is registered **after** the literal `/scans/latest`
and `/scans/latest-diff` paths so those aren't captured as an id.

| Method | Path | Handler |
|---|---|---|
| POST | `/api/scans` | `createScanHandler` — optional body `{"cidr":..., "ports":[...]}` |
| GET | `/api/scans` | `listScansHandler` (summaries) |
| GET | `/api/scans/latest` | `latestScanHandler` |
| GET | `/api/scans/latest-diff` | `latestDiffHandler` |
| GET | `/api/scans/compare?from=&to=` | `compareScansHandler` |
| GET | `/api/scans/{id}` | `getScanHandler` |

## Repo note

This project lives under a larger `goProjects` git repository (the git root is `goProjects/`, not
this directory) that also contains unrelated sibling projects. Scope commits and changes to
`src/netscout/` unless told otherwise.
