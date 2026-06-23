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
