# EasyTier Pro GUI — 项目总结

> 更新日期：2026-08-28

## 一、项目定位

**EasyTier Pro GUI** 是面向 [EasyTier](https://github.com/EasyTier/EasyTier) P2P 组网工具的现代化桌面管理客户端。

- **形态**：Wails（Go）桌面应用 + 内嵌 Web 管理服务。同一套 Vue 界面既运行在原生窗口（WebView2），也可在任意浏览器中远程访问。
- **与上游的关系**：纯包装。不重实现 RPC 协议，通过外部进程方式管理官方 `easytier-core` / `easytier-cli` 二进制（2.6.4）。**版本升级 = 替换核心二进制 + 适配新特性**，GUI 代码与上游解耦。
- **目标用户**：不想用命令行配置 EasyTier 的个人/小团队用户；需要可视化管理多网络、监控拓扑、跨设备远程管理的场景。
- **平台**：当前主打 Windows（管理员权限，虚拟网卡必需）；架构上预留 macOS/Linux（Wails v2 原生支持）。移动端不在范围（Wails v2 无移动端目标）。
- **核心价值**：把 EasyTier 从"配置文件 + 命令行"变成"可视化多网络管理 + 拓扑监控 + 一键分享入网"的桌面产品。

## 二、需求汇总（按提出顺序）

| # | 需求 | 状态 |
|---|------|------|
| 1 | 代码审查：bug、性能、内存泄漏、关闭/退出完整性、跨平台支持 | ✅ |
| 2 | UI 重做为 Brutalist 风格，深色模式适配 | ✅ |
| 3 | 逐个验证所有高级功能开关的有效性（25/26 有效，`ipv6_public_addr_auto` 早期损坏实例，`multi_thread` 为 no-op） | ✅ |
| 4 | 布局大改适应使用习惯；必要时自动授予管理员权限 | ✅ |
| 5 | 多网络选择模型：启动前选择、默认恢复上次运行、运行中切换、多网络共存 | ✅ |
| 6 | 拓扑可视化：直观美观、区分 P2P / Relay 连接、支持多中继 | ✅ |
| 7 | 每个网络独立"启动/停止"按钮；切换网络不再自动跳回 | ✅ |
| 8 | 多网络并行时每个网络一张节点信息卡片；点击卡片切换该网络拓扑 | ✅ |
| 9 | 节点页正确列出不同网络的节点（分组数据展平 + 网络列 + 筛选） | ✅ |
| 10 | 分享链接加入节点（`easytier://` 链接 / TOML 导入，新设备一键入网） | ✅ |
| 11 | 远程管理：监听地址可设 0.0.0.0、端口可配、token 持久化 | ✅ |
| 12 | 日志 / 配置目录路径可选 | ✅ |
| 13 | 自定义 CSS 覆盖 UI 样式 | ✅ |
| 14 | WebDAV 云端同步（全部配置 + 设置一键上传/下载） | ✅ |
| 15 | IPv6 远程分配验证 + 提供者（provider）配置项 | ✅（客户端已验证；provider 仅 Linux） |
| 16 | 管理员权限自动处理（TUN 虚拟网卡必需）：manifest 提权 + 非管理员横幅 + 一键提权重启 | ✅ |
| 17 | 开关网络时命令行窗口完全隐藏 | ✅ |
| 18 | 系统托盘图标，关闭窗口后台驻留（进程不退出，VPN 不掉线） | ✅ |
| 19 | 跨平台（macOS/Linux）打包可行性评估 | 📋 已评估，待实施 |

## 三、已实现功能

### 核心能力
- **核心生命周期管理**：启动/停止/重启 `easytier-core`（隐藏控制台窗口）；支持停止外部遗留核心（RPC 端口定位 + 强制终止）
- **实时状态**：节点信息、对端列表、路由表、流量统计、VPN Portal，通过 `easytier-cli -o json` 查询
- **配置管理**：TOML 配置的增删改查、启用/禁用（`.toml` ⇄ `.toml.disabled`）、instance_id 规范注入、监听端口冲突自动避让

### 多网络模型
- 勾选式选择要运行的网络集合，启动前生效；默认恢复上次运行状态
- 多网络在同一个核心内多实例共存（不同虚拟网卡、DHCP 自动分配不同网段）
- 仪表盘每个运行网络一张实例卡片（网络名/节点 ID/主机名/版本/虚拟 IP/节点数），**点击卡片切换下方拓扑**
- 网络列表内联"启动/停止"按钮，单个网络独立操作

### 拓扑可视化
- 本地中心 + 左 P2P 直连列 + 右 Relay 中继列的分组布局
- P2P 连接（橙色实线）与 Relay 连接（蓝色虚线）明确区分；中继节点实心方块标注
- 多中继自动轮转分配卫星节点；CSS 交错动画；图例说明

### 入网分享
- 一键生成 `easytier://join?network_name=...&network_secret=...&peer=...` 分享链接
- 支持粘贴链接或原始 TOML 导入创建新网络（自动生成实例 ID、避让端口）

### 远程管理与云同步
- 内嵌 Web 管理服务：token 鉴权、监听地址（127.0.0.1 / 0.0.0.0）、端口可配、token 持久化
- WebDAV 同步：全部网络配置 + `settings.json`（含自定义 CSS）打包 zip 上传/下载，本地凭据不丢失
- 存储路径可配置：配置目录、日志目录（改配置目录自动重启核心）

### 系统集成
- 管理员权限：`requireAdministrator` manifest；非管理员时顶部横幅 + "以管理员身份重启"
- 系统托盘：显示主窗口 / 启动-停止核心 / 退出（关闭窗口 = 隐藏到托盘后台驻留）
- 所有子进程（core、cli、netstat、taskkill）控制台窗口完全隐藏

### UI / 前端
- Brutalist 设计系统：硬边框、硬阴影、等宽字体、信号色（橙/蓝/绿/黄/红），零圆角
- 深/浅色主题切换并持久化；自定义 CSS 全局注入（保存即时生效）
- 网络配置全功能表单编辑器（表单 + 原始 TOML 双模式）
- 节点页：多网络分组数据展平、网络列、按网络筛选
- 中英文 i18n

## 四、技术栈

| 层 | 选型 |
|----|------|
| 桌面框架 | Wails v2.10.1（Go 1.23+，实际 1.26.7） |
| 前端 | Vue 3 + Vite 6 + TypeScript + Tailwind CSS 3.4（darkMode: class） |
| 后端 | Go：`internal/core`（进程）/ `internal/easytier`（CLI 封装）/ `internal/configmgr`（配置）/ `internal/webserver`（HTTP API）/ `internal/settings`（持久化） |
| TOML | BurntSushi/toml（Go）、smol-toml（前端） |
| 托盘 | getlantern/systray |
| 内嵌资源 | easytier-core / easytier-cli 2.6.4、wintun.dll / WinDivert64.sys / Packet.dll |

## 五、架构

```
┌──────────────────────────────────────────────────────────┐
│              Wails GUI（Go 后端，管理员权限）                │
│  core.Process  easytier.Client  configmgr  webserver      │
│  settings.Store          systray（系统托盘）                │
│        └──────────┬───────────────┐                       │
│                   ▼               ▼                       │
│        easytier-core（子进程）  easytier-cli -o json        │
│                   ▲               ▲                       │
│  Wails 事件（原生） / HTTP 轮询（浏览器）                    │
│  ┌─────────────────────────────────────────────────────┐  │
│  │  Vue 3 前端：Dashboard / Networks / Peers / Settings │  │
│  └─────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

## 六、已知限制与下一步

- **移动端**：Wails v2 无安卓/iOS 目标。官方移动 App 为 Tauri 实现；如做移动端需 Tauri 重写（Vue 前端可复用大半）
- **macOS/Linux 打包**：待实施项——外部核心停止（taskkill/netstat → kill/lsof）、平台核心二进制打包、驱动按平台区分、托盘图标 PNG、跨平台 CI
- **IPv6**：子网代理（proxy_network）与路由仅支持 IPv4；IPv6 上网转发走 `ipv6_public_addr_auto`，提供者侧仅 Linux 支持
- **多线程**：`multi_thread` 开关在当前核心为 no-op
- **安全**：configmgr 的 instance_id 存在路径遍历风险（`filepath.Join` 未净化），建议加固

## 七、构建与运行

```bash
# 依赖：Go 1.23+、Node 18+、Wails v2 CLI
wails build                    # 输出 build/bin/easytier-pro-gui.exe
wails dev                      # 热重载开发
go test ./... -short           # 单元测试
```

- 首次启动自动解压二进制到 `%APPDATA%\easytier-pro-gui\runtime\`
- 配置目录 `%APPDATA%\easytier-pro-gui\configs\`，日志 `logs\`，设置 `settings.json`，Web 端点 `web.info`
- **必须管理员权限运行**（虚拟网卡 TUN 创建依赖），UAC 弹窗点"是"
