# netscout

A small Go web app that scans your local network for live hosts, stores every scan
in MongoDB, and shows what changed between the two most recent scans.

- **Pure-Go TCP connect scanning** — no `nmap`, no root/raw sockets required.
- **Per host:** IP, up/down status, reverse-DNS hostname, open ports.
- **Full history:** every scan is stored as its own document; nothing is overwritten.
- **Comparison:** diff any two scans, or the latest vs. the previous one, showing hosts
  added, disappeared, and changed (port or status deltas).

## Requirements

- Go 1.26+
- A running MongoDB (local default: `mongodb://localhost:27017`)

## Run

```bash
go build -o netscout .
./netscout
```

Then open <http://127.0.0.1:8092>.

Click **Run scan** (leave the CIDR box blank to auto-detect your local `/24`), then
**Run scan** again later and click **Compare with previous** to see what changed.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `MONGO_URI` | `mongodb://localhost:27017` | MongoDB connection string |

The server listens on `127.0.0.1:8092`. Data is stored in the `netscout` database,
`scans` collection.

Default probed ports: `21, 22, 23, 25, 80, 139, 443, 445, 3306, 3389, 5432, 8080`.

## API

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/scans` | Run + store a scan. Optional body: `{"cidr":"192.168.1.0/24","ports":[22,80]}` |
| GET | `/api/scans` | List scan summaries, newest first |
| GET | `/api/scans/{id}` | Full scan document |
| GET | `/api/scans/latest` | Most recent scan |
| GET | `/api/scans/latest-diff` | Diff the two most recent scans |
| GET | `/api/scans/compare?from={id}&to={id}` | Diff two specific scans |

## How host detection works

Each address in the target range is probed with a TCP connect to the port list.
A host is recorded as **up** if any port either:

- **accepts** the connection (port open), or
- **refuses** it with a TCP RST (port closed, but the host is clearly alive).

This is more complete than open-ports-only, but it still can't see a host that stays
completely silent on every probed port (e.g. a strict firewall dropping all packets).
Such a host will read as absent. Widen the port list per scan if you need more coverage.

## Limitations & scope

- IPv4 only; scans a single CIDR per run.
- TCP connect scanning is slower and less complete than raw-socket tools, but needs no
  privileges.
- **Only scan networks you own or are explicitly authorized to assess.**
