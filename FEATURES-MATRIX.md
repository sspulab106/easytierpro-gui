# Tailscale 功能矩阵对照（EasyTier Pro 实现状态）

> 对照日期：2026-08-31。EasyTier Pro = EasyTier 核心（数据面）+ 本项目 GUI/控制面。
> 「✅ 已实现 / 🟡 部分 / 📋 已规划 / ❌ 依赖上游或超出范围」

## 一、核心网络与连接

| Tailscale 功能 | 状态 | EasyTier Pro 实现 |
|---|---|---|
| P2P Mesh VPN | ✅ | EasyTier 核心（P2P 打洞 + 加密隧道），GUI 全程管理 |
| NAT 穿透 / DERP 中继 | ✅ | EasyTier 内置 STUN 打洞；公共中继 + 自定义 wss 中继（设置页可复制）；对称 NAT 支持有限（上游协议范围） |
| MagicDNS & 私有 IP | ✅ | hosts 文件托管块方案（设备名→虚拟 IP 自动跟随）+ sticky-DHCP 防地址漂移；已实测 |
| Subnet Routers | ✅ | 配置 `proxy_network`（子网代理）+ 系统转发开关 + 物理网卡冲突检测 |
| Exit Nodes | ✅ | 提供端：`enable_exit_node`；**使用端：`exit_nodes` 列表（已实测核心接受）**，配置编辑器高级区可填 |
| 高可用/容灾 | 🟡 | 多中继可配置；多实例共存天然冗余；无自动故障转移编排（📋） |
| 大子网 /8–/32 | ✅ | 已实测 /8、/12、/16、/24（TUN 路由 + 自连通）；提示：DHCP 固定池 10.126.126.0/24，大子网用静态 IP；编辑器实时显示网段归属/容量/重叠警告 |

## 二、身份与访问控制

| Tailscale 功能 | 状态 | 说明 |
|---|---|---|
| IdP / SSO 对接 | 📋 | 本项目自建账号体系（scrypt + 会话 + 限流 + 多用户角色）；OIDC 接入需先把账号存储抽象成接口（已规划） |
| ACL 细粒度访问控制 | 📋 | EasyTier 核心支持 ACL 配置；GUI 的可视化 ACL 编辑器已规划（下一阶段） |
| Tailnet Lock | ❌ | 依赖核心实现分布式节点签名（上游无） |
| JIT 临时提权 | 📋 | 可用 fleet + 临时配置变通；原生审批流已规划 |
| Auth Keys / Ephemeral | 🟡 | 分享链接入网（easytier://）已实现；凭证有效期 + 设备准入清单在设备准入阶段规划 |

## 三、服务发布与应用代理

| Tailscale 功能 | 状态 | 说明 |
|---|---|---|
| Serve（内网服务发布） | ✅ | 虚拟 IP 直达 + Web 管理 + 出口隧道；子网代理覆盖反向场景 |
| Funnel（公网暴露） | ✅ | **公网隧道页**：cloudflared quick tunnel 集成——本机或任意 fleet 受管设备一键开公网 https 域名（trycloudflare.com），URL 自动提取、一键复制、可停；全部端点 admin 门控 |
| App Connectors | ❌ | SaaS 固定出口 IP 场景（可组合 exit_nodes 变通） |
| Taildrop 文件互传 | 📋 | 规划中：基于虚拟网的 P2P 文件传输（agent 内建接收端点） |
| Node Sharing | ✅ | 分享链接跨设备/跨组织加入 |

## 四、特权访问（PAM / SSH）

| Tailscale 功能 | 状态 | 说明 |
|---|---|---|
| 一键 SSH/RDP 直连 | ✅ | Peers 页 + 托盘菜单：复制 SSH 命令 / 直接发起连接 |
| Web SSH 终端 | 📋 | xterm.js + 内建 SSH 代理（已规划，工程量大） |
| SSH 会话录制 | ❌ | 依赖核心透传流量（合规场景再评估） |
| 凭据托管 | ❌ | 超出范围 |

## 五、AI 治理（Aperture）

❌ 整体不属于当前产品范围（P2P VPN 管理工具）；数据面无 L7 解析能力（核心限制）。

## 六、审计与运维

| Tailscale 功能 | 状态 | 说明 |
|---|---|---|
| 审计日志 | ✅ | `audit.jsonl`（登录成败/账号变更/配置变更/核心生命周期/隧道/fleet 指令全打点）+ Web 查看（/api/audit，admin） |
| 流量监控 | ✅ | 10s 采样、24h 曲线、今日/本周合计 |
| 客户端健康指标 | ✅ | **/api/metrics** Prometheus 文本端点（core_up/peers/traffic/fleet/tunnels/sessions/build_info；支持 X-Auth-Token 与 Bearer 两种认证） |
| 设备管理 | ✅ | fleet：agent 心跳、在线状态、远程加入/退出网络、启停核心、令牌撤销、自启检查 |
| 集成（K8s/Terraform） | 📋 | REST API 已全量可用（token 认证 + Bearer），Terraform provider 待做 |
| Headless 服务器部署 | ✅ | `easytier-pro-server` 纯 Go 交叉编译 + systemd 守护一键安装（Tailscale 需第三方 headscale，且无企业控制台） |

## 七、竞品差异化（我们的天然优势）

1. **全栈私有化**：控制面完全自持（数据不出内网），headless 一键部署——金融/政企友好
2. **免费无节点数限制**：无 MAU 计费
3. **管理面 API 全覆盖 + 分级认证**：所有功能均有 REST API（admin/viewer/agent token 三级），可脚本化/CI 化
4. **开放数据面**：底层是开源 EasyTier（可审计），协议兼容官方生态
