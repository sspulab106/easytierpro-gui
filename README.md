# EasyTier Pro GUI

基于 **Wails (Go) + Vue 3 + Tailwind CSS** 的 [EasyTier](https://github.com/EasyTier/EasyTier) 虚拟组网全功能管理器：桌面端（Windows）、内嵌 Web 管理端（任意浏览器/手机）、无界面服务器版（Linux headless）三位一体，共享同一套内核与 API。

与官方 Tauri GUI 的根本区别：**不把 easytier-core 当 Rust 库嵌入**，而是以外部进程方式管理官方 `easytier-core` / `easytier-cli` 二进制——UI 技术栈完全自由，组网协议 100% 官方兼容（`network_name` + `network_secret` + peers 与官方 GUI 可互join同一个网络）。

---

## 一、已完成特性

### 组网核心
- **每网络独立进程**：一个网络一个 easytier-core 进程（官方 GUI 同款架构）——新增/停止/修改任一网络**只影响它自己**，其他网络不断线
- **多网络管理**：创建/编辑/启停/删除多个网络实例，配置存为 EasyTier 原生 TOML（`config-dir` 格式，可互导）
- **全功能配置编辑器**：官方 GUI 的所有字段全覆盖——
  - 基础：实例名/主机名/网络名/密码（明文👁切换）/虚拟 IPv4+DHCP 同行切换（官方布局）
  - 寻址：IPv4/前缀（带网段计算与重叠检测）、IPv6 公网地址自动获取
  - 监听与互联：listeners、初始节点（含一键添加公共中继）、监听映射
  - 子网代理/路由：proxy CIDR、自定义路由、出口节点（提供端+使用端）、网络白名单、socks5
  - VPN Portal（WireGuard）：监听/客户端 CIDR/私钥/命名客户端
  - **端口转发**（`[[port_forward]]`）：本机端口 → 虚拟网设备端口
  - **访问控制 ACL**（`[acl]`）：入站/出站/转发规则链，规则支持协议/端口范围/源/目标 IP 段/允许拒绝
  - TUN 与性能：TUN 名、MTU（带路径 MTU 探测）、接收限速
  - 20+ 高级开关：延迟优先/smoltcp/禁 IPv6/KCP、QUIC 代理与输入/P2P 系列/绑定物理网卡/无 TUN/多线程/加密关闭/打洞系列/UDP 广播中继/UPnP/对称 NAT/魔法 DNS/私有模式等
- **大子网支持**：/8–/32 任意前缀（实测 /8 /12 /16 /24），IP 输入带私网段提示与冲突检测
- **虚拟 IP 记忆（sticky-DHCP）**：网络记住上次分配的虚拟 IP，重启后自动保留，设备加入顺序不再影响地址；异常自动回退 DHCP
- **MagicDNS（hosts 方案）**：把对端主机名写入系统 hosts，`ssh gsjpc` 直接按名字连，IP 变了也没关系
- **防火墙自愈**：TUN 名固定注入（`et_p_<id>`）杜绝官方版每次启动新建随机网卡导致防火墙规则堆积的 bug；内置陈旧规则清理器（实测识别 300+ 条）

### 远程管理
- **Web 管理端**：内嵌 HTTP(S) 服务器 + 完整前端，手机/平板自适应布局
- **账号认证体系**：scrypt 密码哈希、12h 滑动会话、登录限流（5 次失败锁定，倍增至 30 分钟）、多用户 admin/viewer 角色、网络级绑定、设备会话管理（查看/注销单个/注销其他）
- **HTTPS 自签证书**：一键开启 TLS 1.2+；证书自动生成（ECDSA P-256 / 10 年 / SAN 全接口），**agent 端指纹锁定（TOFU）**防中间人，支持轮换与清锁
- **设备管理中枢（fleet）**：任意设备作为 Agent 接入中枢——心跳保活（10s，45s 判离线）、一次性接入令牌（只存哈希）或**中枢账号自助接入**、远程指令：下发/启停网络（自动保留对端本地身份字段）、启停核心、远程开/停公网隧道、删除网络
- **公网隧道**：Cloudflare quick tunnel（cloudflared 一键下载/存放在 runtime 目录）+ **localhost.run SSH 反向隧道**（系统自带 ssh，零下载）双服务商；随机域名自动提取、存活时长实时显示、失败原因展示、历史按日折叠、记录级修改/重启/QR 码
- **审计日志**：登录（含 IP）/账号变更/令牌轮换/配置增删改/核心启停/隧道/fleet 指令全打点，JSONL 落盘（5000 行裁剪），Web 端可查
- **Prometheus 指标**：`/api/metrics` 标准文本格式（Bearer 认证可直接抓取）

### 数据与同步
- **WebDAV 云同步**：全部配置+设置打包 zip 推送/拉取；**每设备字段不外泄**（虚拟 IP/DHCP/主机名恢复时回填本机值）；**账号库 opt-in 同步**（多机同账号互管）；**15 分钟自动同步**（先拉合并再推）
- **流量历史**：按网络采样、按日持久化（JSONL），Dashboard 图表
- **节点探活告警**：离线/高延迟事件 → 系统桌面通知 + 可选 Webhook（钉钉/Server酱/TG 等）
- **自定义 CSS**：UI 样式注入，随云同步
- **主题/语言**：日夜间主题、中英双语

### 构建与部署
- **桌面版**：Windows x64（管理员权限创建 TUN，非管理员有提醒与一键提权重启）
- **服务器版**：Linux amd64/arm64 headless 二进制（`-tags headless`），systemd 安装脚本 `install-server.sh`，SIGTERM 优雅停机
- **全资源自包含**：easytier-core/cli、wintun、WinDivert、Packet.dll 全部内嵌解压，目标机零依赖

---

## 二、快速开始

### 桌面版（Windows）

```
1. 以管理员身份运行 easytier-pro-gui.exe（UAC 允许；托盘图标常驻）
2. 网络 → 创建新网络 → 填网络名/密码 → （可选）填静态 IP 或勾选 DHCP → 保存
3. 启动核心；状态栏/仪表盘查看拓扑
4. 其他设备用官方 GUI 或本软件加入同一网络（同网络名+密码，至少一个公共节点互通）
```

### 无界面服务器（Linux）

```bash
# 上传 easytier-pro-server-linux-amd64 后：
chmod +x easytier-pro-server-linux-amd64
sudo ./install-server.sh easytier-pro-server-linux-amd64   # systemd 守护 + 开机自启
sudo systemctl status easytier-pro-server

# 手动运行（打印 Web 地址 + 引导令牌）
./easytier-pro-server-linux-amd64 --bind 0.0.0.0 --port 56000 --data-dir ~/.local/share/easytier-pro-gui
```

### Web 管理端

```
设置 → Web 管理 → 监听地址 0.0.0.0 + 固定端口（如 56000）→ 保存
浏览器打开 http://<本机IP>:56000（或开启 HTTPS 后 https://…）
首次访问从 web.info / 设置页取引导令牌；设置管理员账号后令牌停止外发，登录页接管
```

### 推荐的后续设置

1. **设置 → 常规与告警**：勾选「开机自启」（托盘常驻，组网随系统在线）
2. **设置 → Web 管理**：勾选「HTTPS（自签证书）」加密所有管理与 agent 通信
3. **设置 → 云同步**：填 WebDAV（坚果云/Nextcloud 等）→ 上传；第二台设备同配置 → 下载；勾选「同步网页账号库」+「每 15 分钟自动同步」实现多机互管
4. **节点页**：勾选魔法 DNS 设备 → 按主机名 SSH/远程桌面

### 常用场景操作

| 想做什么 | 在哪里 |
|---|---|
| 固定每台设备的虚拟 IP | 设置 → 虚拟 IP 记忆（自动）；或网络配置里填静态 IP |
| 把内网服务暴露到公网 | 公网隧道页：选 Cloudflare 或 localhost.run → 填 `http://IP:端口` → 复制公网地址（可扫码） |
| 在另一台设备上开隧道 | 公网隧道页 → 远程设备隧道：选在线受管设备 + 目标 → 公网地址回报到本页 |
| 让 A 电脑管理 B 电脑 | B 设置 → 设备管理中枢：填 A 的 `http(s)://A的IP:56000` + A 的管理员账号 → 保存；A 的「设备」页出现 B，可下发网络/远程启停/开隧道 |
| 限制节点间访问 | 网络编辑 → 访问控制（ACL）：入站/出站/转发链 + 规则 |
| 按名字连接设备 | Peers 页勾选设备启用 MagicDNS（写入 hosts） |

---

## 三、HTTP API

所有功能都有 API；浏览器前端与桌面端共用同一套。

### 认证方式（三选一）
| 方式 | 用法 |
|---|---|
| 会话 Cookie | `POST /api/auth/login` 后自动携带（`et_session`，12h 滑动过期） |
| API 令牌 | 请求头 `X-Auth-Token: <token>`（设置页可复制/轮换） |
| Bearer | `Authorization: Bearer <token>`（Prometheus/脚本友好） |

权限分级：`T`=任意有效凭证；`A`=仅管理员角色。Agent 专用端点用 `X-Agent-Token`（只存哈希）。

### 端点总表

**引导与认证**
| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `GET /webconfig.json` | 无 | 引导：`{auth_required, token?}`；已设管理员账号后不再下发 token |
| `POST /api/auth/login` | 无 | `{username,password}` → 会话（限流保护） |
| `POST /api/auth/logout` | T | 注销当前会话 |
| `GET /api/auth/me` | T | 当前用户/角色 |
| `POST /api/auth/password` | T | 改密/建号 `{current_password,new_password,username}` |
| `GET /api/auth/sessions` | T | 设备会话列表 |
| `POST /api/auth/sessions/revoke` · `/api/auth/sessions/revoke-others` | T | 注销单个 / 注销其他所有会话 |
| `POST /api/auth/rotate-token` | A | 轮换 API 令牌 |

**组网状态与配置**
| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `GET /api/status` | T | 核心/前端版本、Web 地址 |
| `GET /api/node` · `/api/peers` · `/api/routes` · `/api/stats` | T | 节点/对端/路由/流量（按实例分组：`[{instance_id,instance_name,result}]`） |
| `GET /api/vpn-portal` | T | WireGuard 门户信息 |
| `GET /api/configs` · `GET /api/config?id=` | T | 配置列表 / 单个配置原文 |
| `PUT /api/config` · `DELETE /api/config?id=` | T | 保存 / 删除配置 |
| `POST /api/config` | T | 切换配置启停 `{id,enabled}` |
| `POST /api/start` `/api/stop` `/api/restart` | A | 启停/重启核心（全部实例） |
| `POST /api/network-running` | A | 单网络启停 `{id,running}`（不影响其他网络） |
| `GET /api/leases` | T | sticky-DHCP 记忆的虚拟 IP |
| `POST /api/tools/mtu-probe` | T | 路径 MTU 探测 `{target}` |
| `POST /api/tools/notify-test` | A | 发送测试通知（告警链路自检） |
| `GET /api/open-dir?path=` | A | 打开服务器目录（桌面端文件管理） |
| `GET /api/traffic?days=N` | T | 流量历史 |

**账号/用户/审计/指标**
| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `GET/POST/DELETE /api/users` | A | 多用户 CRUD（`{username,password,role,networks}`） |
| `GET /api/audit?limit=N` | A | 审计日志（最新在前，≤5000） |
| `GET /api/metrics` | T | **Prometheus 文本**（`Authorization: Bearer` 或 `X-Auth-Token`） |

**设置/同步/隧道/工具**
| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `GET/PUT /api/settings` | T/A | 应用设置 JSON（`web_https`、`fleet`、`alerts`、`webdav` 等） |
| `POST /api/webdav/push` · `/api/webdav/pull` | A | 云同步上传 / 下载 |
| `GET /api/tunnels` | T | 隧道列表 + cloudflared/ssh 状态 |
| `POST /api/tunnels` | T | 开隧道 `{target, provider: "cloudflared"\|"ssh"}` |
| `PUT /api/tunnels` | T | 修改/重启隧道 `{id,target}`（旧记录被替换） |
| `DELETE /api/tunnels?id=` | T | 停止隧道（保留历史）；`PATCH /api/tunnels` 清空历史 |
| `POST /api/tunnels/install` | T | 下载 cloudflared |
| `GET /api/tunnel-peers` | T | 组网内在线设备（隧道目标选择器） |
| `GET /api/web-tls` | T | HTTPS 状态 + 证书指纹 |
| `POST /api/web-tls/rotate` | A | 轮换自签证书 |
| `POST /api/fleet/clear-pin` | T | 清除本机锁定的中枢证书指纹 |

**设备管理中枢（fleet）**
| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `GET/POST/DELETE /api/fleet` | T | 受管设备列表 / 注册（返回一次性 token）/ 删除 |
| `POST /api/fleet/command` | T | 下发指令 `{agent_id,action,network_id,network_toml,target}` |
| `POST /api/fleet/self-enroll` | T | 账号自助接入（中枢侧） |
| `POST /api/fleet/clear-pin` | T | 清除中枢证书指纹锁（agent 侧） |
| `POST /api/agent/heartbeat` | Agent | 心跳 + 领取待执行指令 |
| `POST /api/agent/result` | Agent | 回报指令执行结果 |

### 指标样例

```text
easytier_pro_core_up 1
easytier_pro_networks_running 2
easytier_pro_peers{network="sspu-ailab"} 3
easytier_pro_traffic_bytes_rx_total{network="sspu-ailab"} 1048576
easytier_pro_fleet_agents_online 1
easytier_pro_tunnels_active 2
easytier_pro_sessions_active 1
```

Prometheus 抓取配置：

```yaml
scrape_configs:
  - job_name: easytier-pro
    metrics_path: /api/metrics
    authorization: { credentials: <API令牌> }
    static_configs: [{ targets: ['127.0.0.1:56000'] }]
```

### curl 示例

```bash
TOKEN=<设置页复制的API令牌>
H="X-Auth-Token: $TOKEN"
BASE=http://127.0.0.1:56000

curl -s -H "$H" $BASE/api/status                       # 状态
curl -s -H "$H" $BASE/api/peers | jq                    # 对端列表
curl -s -H "$H" $BASE/api/metrics                       # Prometheus 指标
curl -s -X POST -H "$H" -H "Content-Type: application/json" \
     -d '{"target":"http://127.0.0.1:3000","provider":"ssh"}' \
     $BASE/api/tunnels                                  # 开 localhost.run 隧道
curl -s -X POST -H "$H" -H "Content-Type: application/json" \
     -d '{"id":"<instance_id>","running":true}' \
     $BASE/api/network-running                          # 启动单个网络
```

---

## 四、架构与项目布局

```
GUI / headless（共享 core 启动器）
 ├─ internal/core      每实例进程组（Group）、路径解析、驱动解压
 ├─ internal/easytier  easytier-cli -o json 封装（状态查询）
 ├─ internal/configmgr TOML CRUD + 共享/设备字段剥离 + 监听端口自愈
 ├─ internal/webserver 内嵌 HTTP(S) API + 静态前端（会话/限流/fleet）
 ├─ internal/auth      scrypt 账号、会话、限流
 ├─ internal/fleet     Agent 存储（令牌哈希、指令队列）
 ├─ internal/tunnel    cloudflared + localhost.run 双服务商隧道管理
 ├─ internal/webcert   自签证书（生成/轮换/指纹）
 ├─ internal/traffic|alerts|lease|audit|hostsfile|settings
 └─ frontend/          Vue 3 + Vite + Tailwind（桌面与 Web 共用）
```

运行时数据目录：`%APPDATA%\easytier-pro-gui\`（Linux：`~/.local/share/easytier-pro-gui/`）
—— `configs/` 网络配置 · `runtime/` 解压的核心二进制与 cloudflared · `logs/` · `webcert/` 自签证书 · `web.info` Web 地址+引导令牌

---

## 五、构建

前置：Go 1.23+、Node 18+、Wails v2 CLI。**注意每个新 shell 都要先 `unset GOROOT`（本机环境曾出现 GOROOT 污染导致构建失败）。**

```bash
wails build              # 桌面版 → build/bin/easytier-pro-gui.exe
wails dev                # 开发热重载

# Linux headless（在 Linux 或 WSL 交叉编译）
GOOS=linux GOARCH=amd64 go build -tags headless -o easytier-pro-server-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -tags headless -o easytier-pro-server-linux-arm64 .

go test ./...            # 全量测试（含真实核心集成测试，勿与运行中的组网同跑）
go test ./... -short     # 跳过需要启动核心/绑端口的集成用例
```

---

## 六、尚未实现的特性（Roadmap）

> 详细矩阵见 `WEB-ROADMAP.md`（Tailscale/官方 easytier-web 对标）与 `FEATURES-MATRIX.md`。

**管理层**
- [ ] **OIDC SSO / 企业 IdP 对接**：账号存储抽象为 IdentityProvider 接口后接入（对应官方 easytier-web 的 `--oidc-*`）
- [ ] **viewer 的网络级数据过滤**：peers/拓扑/流量目前全局只读，未按账号绑定的网络过滤
- [ ] **设备准入清单 + 凭证有效期**：记录合法 peer_id，陌生节点入网告警+一键拉黑（生成 ACL 拒绝规则）；到期凭证自动移入拒绝列表
- [ ] **会话持久化**：应用重启后浏览器会话全部失效，需重新登录
- [ ] **分级文件日志**：trace/debug/info/warn/error 落盘（核心日志有捕获，应用日志未分级）
- [ ] **月度流量配额提醒**：采样数据已具备，缺阈值配置与通知

**组网能力**
- [ ] **内建 DNS（完整 MagicDNS）**：目前用 hosts 文件模拟，尚无内建 DNS 服务器监听 53 端口（`<hostname>` 与 `<hostname>.<网络名>` 搜索域）
- [ ] **ACL 可视化组管理**：规则链 UI 已做，官方的「组管理」（GroupIdentity 声明/成员匹配、`source_groups`/`destination_groups`）未做 UI（可通过 TOML 模式手写）
- [ ] **设备级密钥轮换**：依赖上游核心支持多密钥/密钥版本（上游未提供）

**周边**
- [ ] **Taildrop 式文件互传**
- [ ] **Web SSH / 远程桌面终端**（xterm.js 直连组网设备）
- [ ] **GeoIP 节点地理位置展示**（官方有，依赖 mmdb 数据库，评估不做）
- [ ] **移动端管理 App**（Web 端已做移动适配，暂覆盖）
- [ ] **Terraform / Kubernetes 集成**、**自建中继服务器管理**（EasyTier 自有中继体系，暂用官方/自建 wss 地址）
- [ ] **Windows 服务模式**（`easytier-cli service` 的管理 UI）

---

## 七、文档索引

- `FEATURES-MATRIX.md` — Tailscale 功能对标矩阵
- `WEB-ROADMAP.md` — Web 管理路线图 + 官方 easytier-web 迁移矩阵
- `BUILD-PLATFORMS.md` — 跨平台构建与服务器部署
- `PROJECT.md` — 项目结构与设计决策
- `AGENTS.md` — 代码库协作约定

## 许可

LGPL-3.0（与 EasyTier 兼容）。
