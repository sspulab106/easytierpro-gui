# 跨平台构建指南

EasyTier Pro GUI 支持为 Windows / Linux / macOS 打包，官方核心二进制（v2.4.0 起各平台 zip）已按平台预置在 `resources/` 下，构建时自动嵌入对应平台目录。

## 平台支持矩阵

| 目标平台 | 构建标签 | 嵌入的核心 | 附加驱动 | 状态 |
|---|---|---|---|---|
| Windows x64 | （默认本机构建） | `resources/bin`（2.6.4 core/cli） | `resources/drivers`（wintun 等） | ✅ 主力平台 |
| Linux amd64 | `linux && amd64` | `resources/bin-linux-amd64`（v2.4.0 core/cli） | 无（内核 /dev/net/tun） | ✅ 资源就绪，需在 Linux 上构建 |
| macOS Apple Silicon | `darwin && arm64` | `resources/bin-darwin-arm64`（v2.4.0 core/cli） | 无 | ✅ 资源就绪，需在 macOS 上构建 |
| macOS Intel | `darwin && amd64` | `resources/bin-darwin-amd64`（v2.4.0 core/cli） | 无 | ✅ 资源就绪，需在 macOS 上构建 |
| 其他架构（linux-arm64 等） | `embed_none.go` 兜底 | 不嵌入，回退 PATH 中的系统 easytier-core | 无 | 可构建，需自行安装核心 |

每个平台的 embed 声明在根目录 `embed_*.go`（构建标签隔离），只嵌入本平台目录，不会互相增大体积。

## 各平台构建命令

```bash
# Windows（本机，需 GOROOT 未被污染时先 unset）
unset GOROOT && wails build            # → build/bin/easytier-pro-gui.exe

# Linux（在 Linux 机器或 WSL2 内，需要 GTK/WebKit 开发包）
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
wails build                            # → build/bin/easytier-pro-gui

# macOS（在 macOS 机器上）
wails build                            # → build/bin/EasyTier Pro.app
```

> Wails v2 依赖 cgo（GTK/WebKit），**不能从 Windows 直接交叉编译 Linux/macOS 图形版**；必须在目标平台（或 WSL2/CI）上构建。纯后端包可用 `GOOS=linux go build ./internal/...` 做语法验证。

## 平台差异实现位置

| 功能 | Windows | Linux | macOS |
|---|---|---|---|
| 核心进程终止 | `taskkill /PID`（internal/core/process_windows.go） | SIGTERM→SIGKILL（process_other.go） | 同 Linux |
| 外部核心定位 | `netstat -ano` | `ss -ltnp` | `lsof -nP -iTCP:port` |
| ping 探测/MTU | `ping -n -f -l`（GBK 解码回退） | `ping -M do -s` | `ping -D -s` |
| 开机自启 | HKCU Run 注册表（platform_windows.go） | `~/.config/autostart/*.desktop` | `~/Library/LaunchAgents/*.plist` |
| 桌面通知 | PowerShell 内联 Toast | `notify-send` | `osascript display notification` |
| 管理员/提权 | manifest + UAC（admin_windows.go） | 直接运行（core 需 root 建 TUN，可 sudo 启动） | 直接运行 |
| 数据目录 | `%APPDATA%\easytier-pro-gui` | `~/.local/share/easytier-pro-gui`（XDG） | `~/Library/Application Support/easytier-pro-gui` |
| 应用数据回退 | APPDATA 环境变量 | XDG_DATA_HOME | 无 |

## 无 GUI 服务器（Headless）部署

无桌面环境的 Linux 服务器可用纯 Go 的 headless 二进制（可从 Windows 交叉编译，无需 GTK/cgo）：

```bash
# 在 Windows/任意机器上构建（输出 build/bin/easytier-pro-server-linux-*）
go build -tags headless -trimpath -ldflags "-s -w" -o build/bin/easytier-pro-server-linux-amd64 .

# 传到服务器后一键安装（含 systemd 守护：开机自启 + 崩溃自动重启）
sudo ./install-server.sh easytier-pro-server-linux-amd64
# → Web 管理地址 + 首次令牌打印在安装输出里，之后全部在浏览器管理
```

- headless 与桌面版同一套引擎（Web 管理/核心控制/告警/流量/sticky-DHCP/MagicDNS），无窗口无托盘
- 数据目录 `/var/lib/easytier-pro`（systemd 单元内以 XDG 语义传入）
- flags：`--bind`/`--port`/`--data-dir`/`--version`；SIGTERM 优雅停机并停掉核心
- 设备接入中枢：目标设备 Settings → 设备管理中枢 填中枢地址 + 令牌（设备页可生成）

## 更换/升级核心二进制

1. 从 [EasyTier Releases](https://github.com/EasyTier/EasyTier/releases) 下载目标平台的 zip
2. 解压出 `easytier-core` / `easytier-cli`（GUI 不需要 easytier-web*），覆盖对应 `resources/bin*` 目录
3. 重新 `wails build`——启动时按 SHA-256 对比自动替换 `%APPDATA%` runtime 中的旧版

## Linux 注意事项

- Linux 构建 GUI 无需管理员即可启动，但 `easytier-core` 创建 TUN 需要 root：以 `sudo` 运行 GUI，或将核心注册为 systemd 服务
- 系统托盘需要 `libayatana-appindicator`（Debian/Ubuntu: `libayatana-appindicator3-dev`）
- 部分发行版需 `libnss3`、`libgdk-pixbuf-2.0-0` 等 WebKit 依赖
