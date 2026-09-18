<div align="center">

![RemoraSFTP Logo](./docs/RemoraSFTP.jpg)

# 🐟 RemoraSFTP

**A local-first browser file manager for FTP, FTPS, and SFTP.**

</div>

RemoraSFTP is a single, self-contained executable. The file-management engine
runs entirely on your own computer and is operated through a modern browser
interface served from a loopback address. You double-click the app, your
browser opens, and you connect directly to your FTP, FTPS, or SFTP servers -
no Python, no Node.js, no Docker, no VPS, no cloud account, and no relay in
the middle.

```
 Your browser  →  Local RemoraSFTP engine (this device, 127.0.0.1)  →  Your FTP/SFTP server
```

Uploads and downloads travel **directly between your device and the server
you choose**. There is no RemoraSFTP cloud service involved in core
functionality. The browser is only the graphical interface.

---

## Highlights

- **One binary, double-click to run.** The Go executable embeds the entire web
  UI with `go:embed`. Nothing to install besides the single file.
- **FTP, FTPS, and SFTP** behind one common interface; protocol differences
  are surfaced as explicit **capabilities** instead of being hidden.
- **Security-first local API**: loopback-only bind, per-launch random
  session token, single-use browser bootstrap code, origin validation,
  custom-header CSRF defense, and WebSocket auth via subprotocol
  (tokens never appear in URLs).
- **First-class key authentication**: SFTP private keys, passphrases, and
  SSH-agent support; OS keychain credential storage with an encrypted local
  file fallback.
- **Explicit host-key trust**: SSH host keys and FTPS certificate pins are
  presented for confirmation on first encounter and stored locally. Unknown
  keys are never silently accepted.
- **Desktop-grade file manager**: list/grid views, breadcrumbs, search,
  sorting, multi- and range-selection, drag-and-drop upload, context menus,
  in-place rename, inspector, keyboard navigation, and a command palette.
- **Streaming transfer manager**: progress, speed, ETA, queue, concurrency
  limits, cancel/retry, direct pipe between disk and network (no full-file
  buffering), and isolated temp artifacts that are always cleaned up.
- **Safe preview**: images, video, audio, PDF, text/Markdown/JSON/YAML/TOML/CSV
  with syntax highlighting; remote HTML renders in a fully sandboxed iframe
  (no scripts, no same-origin); server-side code (PHP, Python, etc.) is shown
  as highlighted source and **never executed**.
- **CLI over the same engine**: `start`, `connect`, `ls`, `put`, `get`,
  `mkdir`, `rm`, `mv`, `status`, `version` - no second FTP implementation.
- **Internationalized UI** with English (default, complete) and Bahasa
  Indonesia; new languages are drop-in translation files.
- **Accessible**: semantic controls, ARIA, focus states, keyboard operation,
  live regions, and reduced-motion support.
- **No analytics, no telemetry, no debug endpoints in production builds.**

---

## Quick start (users)

1. Download the RemoraSFTP executable for your platform from the
   [releases](https://github.com/flessan/remorasftp/releases) page.
2. Double-click it (or run `remorasftp start` in a terminal).
3. Your default browser opens automatically at the local interface.
4. Create a connection (protocol, host, credentials), review the host-key
   fingerprint on first connect, and start managing files.

Close the terminal window or press Ctrl+C to stop. All protocol connections
are closed and transfers shut down cleanly.

Windows users: `remorasftp-windows-amd64.exe` runs with a double-click. A
terminal window shows status; future releases will add optional tray mode.

---

## CLI usage

The same binary doubles as a command-line client.

```
remorasftp                       # launch engine + open browser (GUI mode)
remorasftp start [flags]         # start the local engine
remorasftp status                # show whether the engine is running
remorasftp version               # build metadata
remorasftp profiles              # list saved connection profiles
remorasftp connect  <profile>    # connect and print server info
remorasftp ls       <profile> [path]
remorasftp put      <profile> <local-path> <remote-path>
remorasftp get      <profile> <remote-path> <local-path>
remorasftp mkdir    <profile> <remote-path>
remorasftp rm  [-r] <profile> <remote-path>
remorasftp mv       <profile> <from> <to>
```

`start` flags:

| Flag | Meaning |
| --- | --- |
| `--addr <ip>` | listen address (default `127.0.0.1`; loopback only) |
| `--port <n>` | port (default: pick an available one) |
| `--no-browser` | don't launch the browser |
| `--headless` | CLI/automation mode, never opens a browser |
| `--data-dir <path>` | override the application data directory |

Both the browser UI and these CLI commands use the **same** connection
manager, protocol adapters, credential storage, transfer manager, and trust
store - the GUI is just another client of the local engine.

---

## Development

### Requirements

- Go 1.22 or newer
- Node.js 20+ and npm (only to build the embedded UI)

### Common commands

```
make web          # build frontend -> internal/server/webassets
make build        # build the release binary (with ldflags metadata)
make dev          # run the engine on http://127.0.0.1:7970 (no browser)
make web-dev      # Vite dev server (hot reload) with /api proxied to the engine
make test         # build the embedded UI, then run all Go tests
make test-race    # tests with -race
make lint         # go vet + gofmt + frontend typecheck
```

`make test` and `make build` build the frontend first, because the server
package embeds `internal/server/webassets` at compile time — the SPA tests
and the shipped UI need a real `index.html` in that tree. On a fresh
checkout (or any machine with a missing/partial `node_modules`), use
`npm ci` before building so dependencies are installed from the lockfile:

```
cd web
npm ci
npm run build
cd ..
go test ./... -count=1
```

Recommended development loop:

1. Terminal 1: `make dev` (runs the Go engine on port 7970).
2. Terminal 2: `make web-dev` (Vite on port 5180) - opens at
   `http://127.0.0.1:5180` and proxies `/api` to the engine.

For a production-like single binary: `make build`, then `./dist/remorasftp start`.

### Project layout

```
cmd/remorasftp/            entry point (CLI)
internal/
  app/                  engine wiring (config, creds, trust, managers, server)
  apppaths/             OS application data directory
  browser/              default-browser launcher (cross-platform)
  cli/                  command-line interface
  config/               versioned local config + connection profiles
  credentials/          keychain provider + encrypted-file fallback
  events/               in-process event bus + activity/audit log
  manager/              connection/session manager
  protocol/             common Client interface; FTP, FTPS, SFTP adapters
  safepath/             remote path normalization & traversal protection
  server/               loopback HTTP/WebSocket API + embedded SPA
  transfers/            streaming transfer queue/worker pool
  trust/                SSH host key + TLS certificate trust store
  version/              build metadata
web/                    React + TypeScript + Vite browser UI
docs/                   architecture & security documentation
```

---

## Where data lives

All state is stored locally under the OS user configuration directory:

| Platform | Location |
| --- | --- |
| Windows | `%AppData%\RemoraSFTP` |
| macOS | `~/Library/Application Support/RemoraSFTP` |
| Linux | `$XDG_CONFIG_HOME/remorasftp` or `~/.config/remorasftp` |

A `data/` directory next to the executable enables portable mode.

- `config.json` - non-secret profiles, settings, and recents (0600).
- `secrets/` - encrypted fallback credential vault (AES-256-GCM; used when no
  OS keychain is available). On Windows, macOS, and desktop Linux, passwords
  and keys are stored in Credential Manager / Keychain / Secret Service.
- `hostkeys.json` - trusted SSH host keys and pinned FTPS certificates.
- `activity.log.json` - high-level, sanitized event history.
- `tmp/` - isolated temporary transfer artifacts (wiped on startup, shutdown,
  and after every transfer).

Secrets are never placed in URLs, browser history, logs, the DOM, analytics,
or API responses. The browser can write credentials and test for their
presence, but never reads them back.

---

## Documentation

- [Architecture](docs/architecture.md) - the browser/local-engine boundary and
  the protocol adapter model.
- [Security model](docs/security.md) - threat boundaries, local
  authentication, credential handling, and preview isolation.
- [CLI reference](docs/cli.md)

---

## Supported platforms

The engine is written in portable Go and the UI is platform-neutral. Windows
is a first-class build target; Linux and macOS are built from the same source
tree.

```
make build-windows   # dist/remorasftp-windows-amd64.exe
make build-macos     # dist/remorasftp-macos-arm64
make build-linux     # dist/remorasftp-linux-amd64
```

OS-specific behavior (credential backends, browser launching, filesystem
paths) sits behind small platform abstractions so new targets are
straightforward.

---

## Limitations (honest scope)

- Folder downloads in the browser currently transfer individual files rather
  than creating a server-side archive.
- Browser downloads stream to a Blob before the browser's save dialog;
  use the CLI for very large transfers or to bypass browser memory limits.
- Remote server-side copy (`SITE CPFR`/`CPTO` / copy-file extensions) is
  surfaced as a capability but not currently executed; FTP CHMOD relies on
  servers that support `SITE CHMOD`.
- Implicit FTPS and self-signed certificates work via explicit fingerprint
  pinning (same trust UX as SSH host keys).
- Automatic signed self-updates are designed for but not yet enabled; the
  app never phones home for normal operation.

---

## Contributing

Contributions are welcome. Please run `make lint` and `make test` before
submitting a pull request, and do not introduce cloud/relay dependencies or
disable any default security behavior (loopback binding, host-key
verification, secret encryption). See `docs/security.md` for invariants that
must hold in every change.

## License

MIT - see [LICENSE](LICENSE).
