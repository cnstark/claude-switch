# claude_switch

[![Release](https://img.shields.io/github/v/release/cnstark/claude-switch?include_prereleases)](https://github.com/cnstark/claude-switch/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/cnstark/claude-switch)](https://goreportcard.com/report/github.com/cnstark/claude-switch)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

本地反向代理服务器，让 Claude Code 连接 `127.0.0.1:8787` 并按项目粒度路由到不同上游 API。

**关键功能：** 模型路由与主备故障转移 · 熔断器多档退避保护 · 配置热重载无需重启 · Token 用量统计 · 项目间完全隔离 · 流式 SSE 透传

## 目录

- [claude\_switch](#claude_switch)
  - [目录](#目录)
  - [核心概念](#核心概念)
  - [快速安装](#快速安装)
    - [Linux / macOS](#linux--macos)
    - [Windows（PowerShell）](#windowspowershell)
    - [Docker](#docker)
  - [快速开始](#快速开始)
    - [1. 生成私有 key](#1-生成私有-key)
    - [2. 添加上游 API](#2-添加上游-api)
    - [3. 添加项目](#3-添加项目)
    - [4. 添加模型映射](#4-添加模型映射)
    - [5. 启动代理](#5-启动代理)
    - [6. 配置 Claude Code](#6-配置-claude-code)
  - [配置文件详解](#配置文件详解)
    - [配置字段说明](#配置字段说明)
  - [命令参考](#命令参考)
    - [上游管理](#上游管理)
    - [项目管理](#项目管理)
    - [模型映射](#模型映射)
    - [密钥管理](#密钥管理)
    - [代理进程管理](#代理进程管理)
    - [用量统计查询](#用量统计查询)
  - [功能详解](#功能详解)
    - [模型路由与故障转移](#模型路由与故障转移)
    - [allow_direct_access（直接模型名访问）](#allow_direct_access直接模型名访问)
    - [熔断器](#熔断器)
    - [配置热重载](#配置热重载)
    - [Token 用量统计](#token-用量统计)
      - [开启](#开启)
      - [数据存储](#数据存储)
      - [计数语义](#计数语义)
      - [查询](#查询)
    - [日志级别](#日志级别)
  - [部署运维](#部署运维)
    - [systemd 自启](#systemd-自启)
    - [Docker 部署](#docker-部署)
    - [Windows 部署](#windows-部署)
  - [安全](#安全)
  - [技术栈](#技术栈)
  - [从源码构建](#从源码构建)

---

## 核心概念

claude_switch 包含**两类核心对象**：

- **上游（upstream）**：全局共享的 API 连接池。每个上游包含 `{name, url, apikey, model, timeout, retry_backoff}`。`model` 是该上游的**真实模型名**——代理转发时会用此值替换请求体中的模型名。
- **项目（project）**：每个项目含 `{name, log_level, model_map}`。`model_map` 是「Claude Code 请求模型名（别名） → 有序上游列表」的映射。列表顺序即主备故障转移顺序。项目间完全隔离。

```
Claude Code                     claude_switch                      上游 API
┌──────────┐   127.0.0.1:8787   ┌──────────────┐  真实模型名+真实key  ┌──────────────┐
│ 请求模型: │ ────────────────→ │ 路由 → 重写  │ ─────────────────→ │ api.anthropic │
│ opus-4-8  │                   │ 流式透传响应  │ ←───────────────── │ /v1/messages  │
│ key:sk-cs │ ←──────────────── │              │                    └──────────────┘
└──────────┘                   └──────────────┘
```

## 快速安装

### Linux / macOS

```bash
curl -fsSL https://github.com/cnstark/claude-switch/releases/latest/download/install.sh | bash
source ~/.claude_switch/env.sh
```

### Windows（PowerShell）

```powershell
irm https://github.com/cnstark/claude-switch/releases/latest/download/install.ps1 | iex
& $env:USERPROFILE\.claude_switch\env.ps1
```

### Docker

```bash
wget https://raw.githubusercontent.com/cnstark/claude-switch/master/docker-compose.yml
docker-compose up -d
```

> 首次使用请继续阅读下方[快速开始](#快速开始)配置上游和映射。

---

## 快速开始

### 1. 生成私有 key

```bash
cs key gen
# → sk-cs-abcd1234...
```

### 2. 添加上游 API

```bash
cs upstream add cfg1 \
  --url https://api.anthropic.com \
  --apikey sk-ant-xxx \
  --model claude-opus-4-8
```

### 3. 添加项目

```bash
cs project add myproject --key sk-cs-abcd1234... --log-level meta
```

### 4. 添加模型映射

```bash
cs mapping add myproject claude-opus-4-8 cfg1 --backup cfg2
```

> `--backup` 可重复指定多个备用上游，按顺序故障转移。

### 5. 启动代理

```bash
# Linux/macOS（后台守护进程）
cs proxy start

# Windows（前台运行）
cs-proxy.exe
```

### 6. 配置 Claude Code

在 `~/.claude.json` 或项目的 `.claude/settings.json` 中设置：

```json
{
  "apiBaseUrl": "http://127.0.0.1:8787",
  "apiKey": "sk-cs-abcd1234..."
}
```

---

## 配置文件详解

配置文件位于 `~/.claude_switch/config.yaml`（可通过 `CS_CONFIG` 环境变量自定义），完整结构如下：

```yaml
server:
  listen: 127.0.0.1:8787          # 监听地址（默认值，仅绑定本地）
  usage_stats: false              # 是否启用 token 用量统计（默认关闭）
  private_keys:                   # 私有 key → 项目名 映射
    sk-cs-abcd1234...: myproject
    sk-cs-efgh5678...: another-project

upstreams:
  - name: cfg1                    # 上游唯一名称
    url: https://api.xxx1.com # API 基础 URL
    apikey: sk-ant-xxx            # 上游真实 API key
    model: claude-opus-4-8        # 上游真实模型名（转发时替换请求体中的 model 字段）
    timeout: 60s                  # 请求超时时间
    retry_backoff:                # 熔断退避时间（可选，最多 4 档；不配置则不启用熔断）
      - 30s
      - 2m
      - 5m
      - 15m

  - name: cfg2
    url: https://api.xxx2.com
    apikey: sk-xxx
    model: gpt-5
    timeout: 120s
    # 不配置 retry_backoff 则该上游不参与熔断

projects:
  - name: myproject
    log_level: meta               # off | meta | debug
    model_map:                    # 请求模型名 → 有序上游列表（主→备）
      claude-opus-4-8: [cfg1, cfg2]
      claude-sonnet-4-6: [cfg1]

  - name: another-project
    log_level: off
    model_map:
      claude-opus-4-8: [cfg2]
```

### 配置字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `server.listen` | string | 否 | 监听地址，默认 `127.0.0.1:8787` |
| `server.usage_stats` | bool | 否 | 是否启用用量统计，默认 `false` |
| `server.private_keys` | map | 是 | key→项目名映射，至少一条 |
| `upstreams[].name` | string | 是 | 上游唯一标识 |
| `upstreams[].url` | string | 是 | API 基础 URL |
| `upstreams[].apikey` | string | 是 | 上游 API key |
| `upstreams[].model` | string | 是 | 上游真实模型名 |
| `upstreams[].timeout` | duration | 否 | 请求超时，默认 `60s` |
| `upstreams[].retry_backoff` | []duration | 否 | 熔断退避档位，最多 4 档；不配置则关闭熔断 |
| `projects[].name` | string | 是 | 项目唯一标识 |
| `projects[].log_level` | string | 否 | 日志级别：`off`/`meta`/`debug`，默认 `off` |
| `projects[].model_map` | map | 是 | 请求模型名→上游列表 |

---

## 命令参考

### 上游管理

```bash
# 添加上游
cs upstream add <name> --url <url> --apikey <key> --model <model> [--timeout 60s]

# 列出所有上游
cs upstream list

# 删除上游
cs upstream remove <name>

# 更新上游（只传要改的字段）
cs upstream update <name> [--url ...] [--apikey ...] [--model ...] [--timeout ...]
```

### 项目管理

```bash
# 添加项目
cs project add <name> --key <private-key> [--log-level off|meta|debug]

# 列出所有项目
cs project list

# 删除项目（同时删除关联的 private key）
cs project remove <name>
```

### 模型映射

```bash
# 添加映射（可指定多个备用上游）
cs mapping add <project> <request-model> <主用cfg> [--backup <备用cfg>]...

# 查看项目的映射表
cs mapping list <project>

# 删除映射
cs mapping remove <project> <request-model>
```

### 密钥管理

```bash
# 生成私有 key（前缀 sk-cs-）
cs key gen
```

### 代理进程管理

```bash
cs proxy start       # 后台启动（仅 Linux/macOS）
cs proxy status      # 查看运行状态
cs proxy stop        # 停止（仅 Linux/macOS）
cs proxy logs        # 查看日志
cs proxy logs --project myproject --level debug   # 按项目和级别过滤
```

### 用量统计查询

```bash
# 全部项目，最近 7 天（默认）
cs stats

# 指定项目
cs stats myproject

# 自定义时间范围
cs stats --since 30d
cs stats --since 2026-06-01

# 按模型过滤
cs stats myproject --model claude-opus-4-8
```

---

## 功能详解

### 模型路由与故障转移

代理收到请求后的处理流程：

1. **鉴权**：从 `x-api-key` 头（其次 `Authorization: Bearer`）提取 key，恒定时间比较，查出所属项目；未命中返回 `401`
2. **提取模型名**：读取完整请求体，JSON 解析 `model` 字段
3. **查路由表**：在该项目的 `model_map` 中查 `model` → 有序上游列表；未命中返回 `404`
4. **熔断检查**：跳过处于退避状态的上游（详见[熔断器](#熔断器)）
5. **故障转移循环**：逐个尝试上游列表中的上游：
   - 重写请求体中的 `model` 为上游真实模型名
   - 替换 `x-api-key` 为上游 API key
   - 流式 POST 到 `<URL>/v1/messages`
   - 成功 → 返回；连接失败/超时/5xx/429 → 记录失败并尝试下一个
6. **流式透传**：使用 `http.Flusher` 逐 chunk（4KB 缓冲区）透传 SSE 响应，不缓冲完整响应体

> **重要**：一旦开始向客户端写入响应（首字节已发送），不再切换上游——流式已开始后无法回退。

### allow_direct_access（直接模型名访问）

项目开启 `allow_direct_access: true` 后，请求中的 `model` 字段若等于某个 upstream 的 `name`（cfg 名），将直接路由到该上游，无需在 `model_map` 中配置别名。

**约束：** `model_map` 的别名不得与任何 `upstream.name` 相同，否则校验失败（保证路由无歧义）。

```yaml
projects:
  - name: default
    allow_direct_access: true
    model_map:
      claude-opus: [anthropic]
```

可用 `cs project direct-access <name> <on|off>` 切换。

### 熔断器

熔断器通过多档退避机制保护上游服务，避免故障上游被频繁重试：

**工作原理：**

- **触发条件**：同一上游连续失败 2 次后进入退避
- **退避档位**：最多 4 档，每档的等待时间由 `retry_backoff` 配置（如 `[30s, 2m, 5m, 15m]`）
- **状态转换**：
  - 正常（L0）→ 连续失败 2 次 → 进入 L1，等待 30s
  - L1 到期 → 放行一次探测：成功则恢复正常，失败则升级到 L2（等待 2m）
  - 依此类推至 L4；L4 失败后循环回 L1
- **全部跳过时的强制探测**：当模型映射中所有上游都在退避时，强制对第一个上游放行一次探测，避免永久锁死
- **不启用**：不配置 `retry_backoff` 或留空，该上游不参与熔断

```bash
# 添加上游时在 config.yaml 中配置 retry_backoff
# 或通过 update 更新：
cs upstream update cfg1 --url ... --apikey ... --model ... --timeout ...
# retry_backoff 目前需直接在 config.yaml 中编辑
```

### 配置热重载

代理运行时修改 `~/.claude_switch/config.yaml` 后**自动检测并加载**，无需重启：

- 每 2 秒轮询文件修改时间（mtime）
- 变更时重新解析 YAML → 校验 → 原子替换运行时快照
- **校验失败保留旧配置**，错误信息输出到 stderr，不影响在途请求
- 每个请求从当前快照重建鉴权表、路由表和日志级别——天然支持热重载

### Token 用量统计

代理可记录每个请求的 token 用量，按 project/model/date 汇总，便于成本核算与用量分析。默认**关闭**，需手动开启。

#### 开启

在配置文件的 `server` 节添加 `usage_stats: true`：

```yaml
server:
  listen: 127.0.0.1:8787
  usage_stats: true
  private_keys:
    sk-cs-abcd1234...: myproject
```

开关支持**热重载**——修改后无需重启即生效。关闭时不再产生新记录，但历史数据保留，可随时重新开启。

#### 数据存储

用量数据持久化到 `~/.claude_switch/usage.json`：

- 代理启动时加载历史数据，后台每 10 秒刷盘一次，退出时执行最终 flush
- 原子写入（临时文件 + `rename` 覆盖），刷盘失败保留 dirty 标记下次重试
- 记录过程对请求转发完全透明（fail-soft：解析异常丢弃该行，采集异常被 recover 兜底，均不影响代理正常工作）

#### 计数语义

- **仅在上游返回 usage 字段时计数**（解析 Anthropic 响应中的 `message_start` / `message_delta` SSE 事件）
- **故障转移只计成功的那一个上游**：连接失败 / 超时 / 5xx / 429 触发重试时不计
- 按**响应结束日**归档到对应日期
- 记录四个维度：`input_tokens` / `output_tokens` / `cache_creation_input_tokens` / `cache_read_input_tokens`

#### 查询

```bash
cs stats                    # 全部项目，最近 7 天
cs stats myproject          # 指定项目
cs stats --since 30d        # 最近 30 天
cs stats --since 2026-06-01 # 从指定日期开始
cs stats myproject --model claude-opus-4-8  # 按模型过滤
```

输出列：`PROJECT | MODEL | DATE | INPUT | OUTPUT | CACHE_CREATE | CACHE_READ | TOTAL`

> 暂无数据时显示 `（暂无用量数据）`（文件不存在不报错）；若 `usage.json` 损坏则报错退出。

### 日志级别

每个项目可独立设置日志级别：

| 级别 | 说明 |
|------|------|
| `off` | 不输出日志（默认） |
| `meta` | 记录请求元信息：鉴权结果、路由决策、上游选择、熔断状态变更、token 用量 |
| `debug` | 在 meta 基础上记录完整请求体和响应体（⚠️ 含 API key，排查后请及时关闭） |

日志格式为结构化 JSON，输出到代理进程的 stderr（`cs proxy logs` 可查看）。

---

## 部署运维

### systemd 自启

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

### Docker 部署

```bash
# 下载 compose 文件
wget https://raw.githubusercontent.com/cnstark/claude-switch/master/docker-compose.yml

# 启动
docker-compose up -d

# 查看日志
docker-compose logs -f

# 在容器内执行 CLI 管理命令
docker-compose exec cs-proxy cs key gen
docker-compose exec cs-proxy cs upstream add cfg1 ...

# 停止
docker-compose down
```

> 容器默认入口为 `cs-proxy`，配置文件通过 volume 挂载到宿主机 `~/.claude_switch`。
> 也可在宿主机直接用 `cs` 操作共享的 `config.yaml`（推荐，目录已挂载）。
> 使用 `ghcr.io/cnstark/claude-switch:latest` 镜像，首次启动自动拉取。

### Windows 部署

Windows 下 `cs proxy start/stop` 使用 Unix 信号机制，**无法正常工作**。请直接前台运行：

```powershell
# 前台启动
cs-proxy.exe

# 或通过环境变量指定配置文件
$env:CS_CONFIG = "$env:USERPROFILE\.claude_switch\config.yaml"
cs-proxy.exe
```

后台运行方案：

```powershell
# 方案 1：nssm 注册为 Windows 服务
nssm install cs-proxy "%USERPROFILE%\bin\cs-proxy.exe"
nssm start cs-proxy

# 方案 2：Windows 计划任务
# 创建触发器为"系统启动时"的基本任务，操作为启动 cs-proxy.exe
```

---

## 安全

- 配置文件包含上游 API key，**必须** `chmod 600 ~/.claude_switch/config.yaml`
- `debug` 日志级别会落盘凭证，排查后请及时切回 `meta` 或 `off`
- 代理**仅监听 `127.0.0.1`**，不暴露外网——这是设计约束，请勿修改为 `0.0.0.0`
- 鉴权使用 `crypto/subtle.ConstantTimeCompare` 恒定时间比较，防止时序攻击
- 所有错误响应统一为 Anthropic 兼容格式，不泄露内部配置信息

---

## 技术栈

- **语言**：Go 1.26
- **依赖**：`github.com/spf13/cobra`（CLI 框架）+ `gopkg.in/yaml.v3`（YAML 解析），其余为标准库
- **协议**：Anthropic Messages API 兼容上游
- **架构**：双二进制（`cs` CLI 管理工具 + `cs-proxy` 代理守护进程）

## 从源码构建

```bash
git clone https://github.com/cnstark/claude-switch.git
cd claude-switch

# Linux/macOS
make build          # → bin/cs, bin/cs-proxy
make build-all      # 全平台交叉编译

# Windows
go build -o bin/cs.exe ./cmd/cs
go build -o bin/cs-proxy.exe ./cmd/cs-proxy

# 运行测试
make test           # 或 go test ./...
```

> 构建需要 Go 1.26+。
