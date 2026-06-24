# claude_switch

本地反向代理服务器，让 Claude Code 连接 `127.0.0.1:8787` 并按项目粒度路由到不同上游 API。

## 安装

### Linux / macOS

```bash
cd claude_switch
go build -o ~/bin/cs ./cmd/cs
go build -o ~/bin/cs-proxy ./cmd/cs-proxy
```

### Windows

```powershell
cd claude_switch
go build -o %USERPROFILE%\bin\cs.exe ./cmd/cs
go build -o %USERPROFILE%\bin\cs-proxy.exe ./cmd/cs-proxy
```

> 也可用 PowerShell 变量：`go build -o $env:USERPROFILE\bin\cs.exe ./cmd/cs`

## 启动

### Linux / macOS

```bash
# 后台守护进程（推荐）
cs proxy start
cs proxy status
cs proxy logs
cs proxy stop
```

### Windows

Windows 下 `cs proxy start` 使用了 Unix 特有的进程管理（`os.StartProcess` + `SIGTERM`），**无法正常工作**。请直接前台运行：

```powershell
# 前台启动代理（推荐）
cs-proxy.exe
```

也可通过环境变量指定配置文件路径：

```powershell
$env:CS_CONFIG = "$env:USERPROFILE\.claude_switch\config.yaml"
cs-proxy.exe
```

> 如需后台运行，可将以下命令注册为 **Windows 计划任务** 或 **nssm 服务**：
> ```powershell
> # 示例：使用 nssm 注册为系统服务
> nssm install cs-proxy "%USERPROFILE%\bin\cs-proxy.exe"
> nssm start cs-proxy
> ```

## 快速开始

```bash
# 1. 生成私有 key
cs key gen
# → sk-cs-abcd1234...

# 2. 添加上游
cs upstream add cfg1 --url https://api.anthropic.com \
  --apikey sk-ant-xxx --model claude-opus-4-8

# 3. 添加项目
cs project add myproject --key sk-cs-abcd1234... --log-level meta

# 4. 添加模型映射
cs mapping add myproject claude-opus-4-8 cfg1

# 5. 启动代理
cs proxy start

# 6. 配置 Claude Code
# 在 ~/.claude.json 或项目 .claude/settings.json 中:
# "apiBaseUrl": "http://127.0.0.1:8787"
# "apiKey": "sk-cs-abcd1234..."
```

## 命令参考

### 上游管理
```
cs upstream add <name> --url <url> --apikey <key> --model <model> [--timeout 60s]
cs upstream list
cs upstream remove <name>
cs upstream update <name> [--url ...] [--apikey ...] [--model ...]
```

### 项目管理
```
cs project add <name> --key <private-key> [--log-level off|meta|debug]
cs project list
cs project remove <name>
```

### 模型映射
```
cs mapping add <project> <request-model> <cfg-name> [--backup <cfg-name>]...
cs mapping list <project>
cs mapping remove <project> <request-model>
```

### 密钥管理
```
cs key gen
```

### 守护进程
```
cs proxy start     # 后台启动
cs proxy status    # 检查状态
cs proxy stop      # 停止
cs proxy logs      # 查看日志
cs proxy logs --project myproject --level debug  # 筛选
```

## 配置热重载

代理运行时修改 `~/.claude_switch/config.yaml` 后自动检测并加载，无需重启。校验失败时保留旧配置。

## Token 用量统计

代理可记录每个请求的 token 用量（`input` / `output` / `cache_creation` / `cache_read`），按 project / model / date 汇总，便于成本核算与用量分析。默认**关闭**，需手动开启。

### 开启

在 `~/.claude_switch/config.yaml` 的 `server` 节添加 `usage_stats: true`：

```yaml
server:
  listen: 127.0.0.1:8787
  usage_stats: true          # 默认 false，开启后开始记录
  private_keys:
    sk-cs-abcd1234...: myproject
```

开关支持**热重载**——修改后无需重启即生效。关闭时不再产生新记录，但历史数据保留，可随时重新开启继续累积。

### 数据存储

用量持久化到 `~/.claude_switch/usage.json`（与 `config.yaml` 同目录）：

- 代理启动时加载历史用量，后台每 10 秒刷盘一次，退出时执行最终 flush
- 原子写入（临时文件 + `rename` 覆盖），刷盘失败保留 dirty 标记下次重试，不会丢失计数
- 记录过程对请求转发完全透明（fail-soft：解析异常丢弃该行，采集异常被 recover 兜底，均不影响代理正常工作）

### 计数语义

- **仅在上游返回 usage 字段时计数**（解析 Anthropic 响应中的 `message_start` / `message_delta`）
- **故障转移只计成功的那一个上游**：连接失败 / 超时 / `5xx` / `429` 触发重试时不计，只有最终成功的 cfg 计一次
- 每个请求统计一次，按**响应结束日**归档到对应日期

### 查询用量

`cs stats` 读取 `usage.json` 并按表格输出，与代理进程及开关**无关**（随时可查，即便统计未开启）：

```bash
# 全部项目最近 7 天（默认）
cs stats

# 查看指定项目
cs stats myproject

# 自定义时间区间：1d / 7d / 30d 或 YYYY-MM-DD
cs stats --since 30d
cs stats --since 2026-06-01

# 按模型过滤
cs stats myproject --model claude-opus-4-8
```

输出列：`project | model | date | input | output | cache_creation | cache_read | total`。

> 暂无数据时打印 `（暂无用量数据）`（文件不存在不报错）；若 `usage.json` 损坏则报错退出。

## systemd 自启

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

## 安全

- 配置文件包含上游 API key，必须 `chmod 600 ~/.claude_switch/config.yaml`
- debug 日志级别会落盘凭证，排查后请及时关闭
- 代理仅监听 `127.0.0.1`，不暴露外网

## 技术栈

Go 1.26, net/http, gopkg.in/yaml.v3, spf13/cobra
