# AGENTS.md

Wails v2 (Go backend) + Vue 3 + Tailwind GUI that manages the official
`easytier-core.exe` / `easytier-cli.exe` binaries as external processes.
Not a git repo. Windows-only (win32, PowerShell 5.1 shell).

## Environment gotchas (critical)

- **GOROOT is corrupted at the user level** — points to a stale temp toolchain
  (`C:\Users\admin\AppData\Local\Temp\opencode\go-sdk3\go`) which breaks every
  Go command (`go test`, `wails build`) with "package testing is not in std" /
  version-mismatch errors. **Always prefix Go commands with:**
  `$env:GOROOT = "C:\Program Files\Go"`
- Source files must stay **UTF-8**. Never write non-ASCII (Chinese) into source
  via PowerShell `Set-Content`/here-strings (writes GBK → mojibake like
  `涓枃`). Use the edit/write tools instead. If a file is already corrupted,
  fix at the byte level.
- Elevated (`RunAs`/admin) processes **cannot be killed from a non-elevated
  shell** ("access denied"). To restart an admin app, launch a
  `powershell -Verb RunAs` script that does `Stop-Process` + `Start-Process`
  (triggers a UAC prompt the user grants).

## Build

```powershell
$env:GOROOT = "C:\Program Files\Go"
# frontend typecheck + bundle (vue-tsc --noEmit && vite build)
npm run build   # in frontend/
wails build     # produces build/bin/easytier-pro-gui.exe
```

`main.go` embeds `frontend/dist` via `//go:embed` — frontend changes require a
full `wails build` to take effect.

## Test commands

- Go: `$env:GOROOT = "C:\Program Files\Go"; go test ./...`
  - `internal/core`'s `TestStartCoreAndQuery` is a real integration test that
    spawns easytier-core (needs bundled binaries under `resources/`; paths are
    hardcoded to `D:\easytierpro\easytier-pro-gui\resources` in the test).
    It fails if a leftover `easytier-core` process holds ports — kill all
    easytier processes first.
- UI tests (`tests/ui/*.py`, Playwright + Python): the app must be **running**
  with its embedded web server. The port is random per launch; tests read
  `%APPDATA%\easytier-pro-gui\web.info` via `tests/ui/common.py`.
  **Run them sequentially, never in parallel** — they share the one live app
  instance and interfere (create/delete configs, start/stop core).
  ```powershell
  python tests/ui/ui_smoke.py
  python tests/ui/ui_multi_network.py
  python tests/ui/ui_e2e.py
  ```

## Architecture

- `main.go` / `app.go` — Wails entrypoint + the `App` struct (all Go→JS bindings).
- `internal/webserver/server.go` — embedded HTTP API + static frontend serving,
  random `127.0.0.1` port, token auth via `X-Auth-Token` header (token in
  `web.info`). Browser mode talks to `/api/*`; native WebView talks to Go
  bindings directly.
- `internal/configmgr` — TOML config CRUD in easytier's `config-dir` format.
- `internal/core` — easytier-core subprocess lifecycle; `RpcPortal` is mutable
  (`SetRpcPortal`) so tests isolate instances.
- `internal/easytier` — `easytier-cli -o json` wrapper.
- `resources_embed.go` — extracts bundled binaries/drivers into
  `%APPDATA%\easytier-pro-gui\runtime\` on first launch.

## Runtime data (outside repo)

- Configs: `%APPDATA%\easytier-pro-gui\configs\` (enabled=`*.toml`,
  disabled=`*.toml.disabled`)
- Logs: `%APPDATA%\easytier-pro-gui\logs\easytier.log`
- Web endpoint/token: `%APPDATA%\easytier-pro-gui\web.info`

## easytier config format rules (verified against easytier-core 2.6.4)

- **Peers must be `[[peer]]` tables**, e.g. `[[peer]]\nuri = "wss://ez.cloud.c01.kr"`.
  A top-level `peers = [...]` array is silently ignored by core in config-dir
  mode (no connection, no error). `frontend/src/lib/network.ts` handles both
  parse/serialize — keep it that way.
- `instance_id` **must be a valid UUID** or core refuses to load the config
  ("UUID parsing failed"). The UI auto-generates one.
- Multi-network coexistence: each instance needs a distinct listener port
  (core binds `0.0.0.0:port`). The UI auto-assigns the next free port
  (11010 → 11011 → …) and warns on manual conflicts
  (`frontend/src/lib/network.ts: listenerPorts/freeListenerPort`).
- Official public relay is `wss://ez.cloud.c01.kr` (port 443, no explicit
  port). `wss://ez.cloud.c01.kr:11012` does **not** connect.
- TUN device creation requires the app (and thus easytier-core) to run **as
  administrator**. Without admin, core runs fine in `no_tun` mode but wintun
  fails with "Failed to create adapter".
- Config files are UTF-8; Chinese strings in the UI come from `i18n.ts`.

## Testing quirks

- `internal/core` tests spawn real easytier-core on random RPC ports
  (`WithTestPort`); they can collide with a running app instance.
- Browser-mode API returns empty 200 (`null`/`[]`) when core is down via
  `isCoreDown` in `server.go` — do not revert to 500 (UI polls these and
  logs console errors).
- The `tests/ui/ui_*` scripts target the live app and assert on text/state
  that changes slowly; they use generous `wait_for_timeout` — don't "fix"
  these by shortening waits.
