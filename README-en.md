# claude_switch

[![Release](https://img.shields.io/github/v/release/cnstark/claude-switch?include_prereleases)](https://github.com/cnstark/claude-switch/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/cnstark/claude-switch)](https://goreportcard.com/report/github.com/cnstark/claude-switch)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A local reverse proxy server that lets Claude Code connect to `127.0.0.1:8787` and routes requests to different upstream APIs by project.

## Quick Install

### Linux / macOS

```bash
curl -fsSL https://github.com/cnstark/claude-switch/releases/latest/download/install.sh | bash
source ~/.claude_switch/env.sh
```

### Windows (PowerShell)

```powershell
irm https://github.com/cnstark/claude-switch/releases/latest/download/install.ps1 | iex
& $env:USERPROFILE\.claude_switch\env.ps1
```

### Docker

```bash
# Download docker-compose.yml
wget https://raw.githubusercontent.com/cnstark/claude-switch/master/docker-compose.yml
# Start
docker-compose up -d
```

> First-time users: continue reading [Quick Start](#quick-start) below to configure upstreams and mappings.

## Build from Source

### Linux / macOS

```bash
git clone https://github.com/cnstark/claude-switch.git
cd claude-switch
make build
```

### Windows

```powershell
git clone https://github.com/cnstark/claude-switch.git
cd claude-switch
go build -o %USERPROFILE%\bin\cs.exe ./cmd/cs
go build -o %USERPROFILE%\bin\cs-proxy.exe ./cmd/cs-proxy
```

## Start Proxy

### Linux / macOS

```bash
# Background daemon (recommended)
cs proxy start
cs proxy status
cs proxy logs
cs proxy stop
```

### Windows

`cs proxy start` on Windows uses Unix-specific process management (`os.StartProcess` + `SIGTERM`) and **cannot work properly**. Run in foreground instead:

```powershell
# Foreground proxy (recommended)
cs-proxy.exe
```

Optionally specify config path via environment variable:

```powershell
$env:CS_CONFIG = "$env:USERPROFILE\.claude_switch\config.yaml"
cs-proxy.exe
```

> To run in background, register as a **Windows Scheduled Task** or **nssm service**:
> ```powershell
> # Example: register as system service with nssm
> nssm install cs-proxy "%USERPROFILE%\bin\cs-proxy.exe"
> nssm start cs-proxy
> ```

## Quick Start

```bash
# 1. Generate private key
cs key gen
# → sk-cs-abcd1234...

# 2. Add upstream
cs upstream add cfg1 --url https://api.anthropic.com \
  --apikey sk-ant-xxx --model claude-opus-4-8

# 3. Add project
cs project add myproject --key sk-cs-abcd1234... --log-level meta

# 4. Add model mapping
cs mapping add myproject claude-opus-4-8 cfg1

# 5. Start proxy
cs proxy start

# 6. Configure Claude Code
# In ~/.claude.json or project .claude/settings.json:
# "apiBaseUrl": "http://127.0.0.1:8787"
# "apiKey": "sk-cs-abcd1234..."
```

## Command Reference

### Upstream Management
```
cs upstream add <name> --url <url> --apikey <key> --model <model> [--timeout 60s]
cs upstream list
cs upstream remove <name>
cs upstream update <name> [--url ...] [--apikey ...] [--model ...]
```

### Project Management
```
cs project add <name> --key <private-key> [--log-level off|meta|debug]
cs project list
cs project remove <name>
```

### Model Mapping
```
cs mapping add <project> <request-model> <cfg-name> [--backup <cfg-name>]...
cs mapping list <project>
cs mapping remove <project> <request-model>
```

### Key Management
```
cs key gen
```

### Daemon Management
```
cs proxy start     # Start in background
cs proxy status    # Check status
cs proxy stop      # Stop
cs proxy logs      # View logs
cs proxy logs --project myproject --level debug  # Filter
```

## Hot Reload

The proxy automatically detects changes to `~/.claude_switch/config.yaml` at runtime and reloads without restarting. On validation failure, the old config is retained.

## Token Usage Statistics

The proxy records token usage per request (`input` / `output` / `cache_creation` / `cache_read`), aggregated by project/model/date for cost accounting and usage analysis. Disabled by **default**; must be manually enabled.

### Enable

Add `usage_stats: true` to the `server` section in `~/.claude_switch/config.yaml`:

```yaml
server:
  listen: 127.0.0.1:8787
  usage_stats: true          # default: false
  private_keys:
    sk-cs-abcd1234...: myproject
```

The switch supports **hot reload** — takes effect immediately without restarting the proxy. When turned off, no new records are generated, but historical data is preserved and can be resumed by re-enabling.

### Data Storage

Usage data is persisted to `~/.claude_switch/usage.json` (same directory as `config.yaml`):

- Loads historical data on startup, flushes to disk every 10 seconds in background, performs a final flush on exit
- Atomic writes (temp file + `rename`), dirty markers on flush failure for retry — no data loss
- Recording is fully transparent to request forwarding (fail-soft: malformed lines discarded, parse panics recovered)

### Counting Semantics

- **Only counted when upstream returns usage fields** (parsed from Anthropic `message_start` / `message_delta` events)
- **Failover only counts the successful upstream**: connection failures / timeouts / `5xx` / `429` are not counted, only the final successful cfg counts once
- Each request counted once, archived to the date of **response completion**

### Query

`cs stats` reads `usage.json` and outputs a table, independent of the proxy process and the usage switch:

```bash
# All projects, last 7 days (default)
cs stats

# Specific project
cs stats myproject

# Custom time range: 1d / 7d / 30d or YYYY-MM-DD
cs stats --since 30d
cs stats --since 2026-06-01

# Filter by model
cs stats myproject --model claude-opus-4-8
```

Output columns: `project | model | date | input | output | cache_creation | cache_read | total`.

> Prints `(No usage data)` when empty (no error if file doesn't exist); errors and exits if `usage.json` is corrupted.

## systemd Auto-Start

```ini
# ~/.config/systemd/user/cs-proxy.service
[Unit]
Description=claude_switch proxy
After=network.target

[Service]
ExecStart=%h/bin/cs-proxy
Restart=on-failure
Environment=CS_CONFIG=%h/.claude_switch/config.yaml

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now cs-proxy
```

## Security

- Config file contains upstream API keys; must `chmod 600 ~/.claude_switch/config.yaml`
- Debug log level writes credentials to disk; disable after troubleshooting
- Proxy only listens on `127.0.0.1`, never exposed to external network

## Tech Stack

Go 1.26, net/http, gopkg.in/yaml.v3, spf13/cobra
