# Web 管理端对标路线图（Tailscale / ZeroTier）

> 目标：把 EasyTier Pro 的管理平面做到接近 Tailscale Admin Console 的体验。
> 边界说明：**管理面认证 ≠ 数据面准入**——设备加入组网的门禁是 network_secret；
> 设备级准入控制需要依赖核心 ACL/白名单能力（管理面下发 + 重启生效）。

## 已完成

### 认证与会话（第一轮）
| 能力 | 对标 | 实现方式 |
|---|---|---|
| 网页管理账号密码登录 | Tailscale SSO（本地版） | scrypt 密码哈希 + 12h 滑动会话 Cookie + 登录限流（5 次失败锁 5 分钟起，倍增至 30 分钟） |
| 引导接口封堵 | — | 设置账号后 `/webconfig.json` 不再发放访问令牌；知道地址 ≠ 能管理 |
| API 令牌轮换 | Tailscale auth key | `POST /api/auth/rotate-token`，旧令牌立即失效 |
| MagicDNS（轻量版） | Tailscale MagicDNS | 定时把在线节点 主机名→虚拟IP 写入系统 hosts 文件管理块；`ssh gsjpc` 按名访问，IP 变化自动跟随（已实测） |
| Sticky-DHCP | Tailscale 固定 IP | 每网络记忆上次分配的虚拟 IP，下次启动自动保留，冲突自动回退（已实测） |
| 节点告警 | — | 离线/高延迟 → 系统通知 + Webhook |
| 流量历史 | Tailscale 用量页 | 10s 采样落盘，24h 曲线 + 今日/本周合计 |

### 多用户与设备管理（第二轮）
| 能力 | 对标 | 实现方式 |
|---|---|---|
| 多账号 + 角色 | Tailscale users | 账号存储（`web-accounts.json`）：admin 全权 / viewer 只读（写操作 403），独立「用户管理」页面增删改、重置密码 |
| 账号 ↔ 网络绑定 | Tailscale ACL 雏形 | 每账号可绑定可见网络集合（配置列表按此过滤）；空 = 全部 |
| 设备会话管理 | Tailscale machines | 独立「设备会话」页面：所有活跃登录（用户/IP/浏览器/登录时间/最后活跃），admin 可注销任意设备，任何人可注销自己的其他会话 |
| 删除账号即踢下线 | — | 删除用户时其全部会话立即失效 |
| Web 端适配 | — | 浏览器端整体缩放（html.web）+ 手机端底部导航条/紧凑布局；拓扑图高度上限 || 同步过滤（每设备隔离） | — | 归档剥离配置中的 ipv4/hostname/dhcp（恢复时按 instance_id 回填本机值），settings 只带自定义 CSS + 告警规则；令牌/路径/账号/DHCP 记忆永不出机 |
| 用户管理独立页 | Tailscale users | 已完成第二轮，见上 |

### 云同步验证
- 临时 WebDAV 服务器（`cmd/davtest`，x/net/webdav + Basic auth）实测：push 上传备份 zip（含 configs/ + settings.json）→ pull 恢复 2 个配置，往返一致。

## TODO（按优先级）

1. **viewer 的网络级数据过滤**：peers/拓扑/流量按账号绑定的网络过滤（现在只过滤配置列表，监控数据仍是全局只读）
2. **设备准入清单**（第三阶段核心）：记录每网络合法 peer_id；陌生节点入网 → 告警 + 一键拉黑（生成 ACL 拒绝规则 + 重启核心）；"凭证有效期"到期移入拒绝列表，重新认证恢复
3. **审计日志**：登录成功/失败、配置变更、网络启停、账号与设备管理事件落盘 JSONL + 管理页查看
4. **内建 DNS**（完整 MagicDNS）：GUI 监听 DNS 端口，支持 `<hostname>` 与 `<hostname>.<网络名>` 搜索域；升级或替代 hosts 方案
5. **ACL 可视化编辑器**：规则表（源/目的/端口/协议/允许拒绝）→ 生成核心 ACL 配置
6. **网络分组/标签**、**月度流量配额提醒**（采样数据已具备）
7. **VPN Portal（WireGuard）管理页增强**：客户端一键生成配置下载
8. **WebDAV 定时自动备份**（手动 push/pull 已有；加 cron 式自动推送 + 保留 N 份历史）
9. **会话持久化**：GUI 重启后会话目前全部失效（需重新登录）；可选落盘恢复
10. **真正的 SSO（OIDC）**：把账号存储抽象成接口后可对接企业 IdP

## 暂不做（依赖上游 / 不适用）

- 设备级密钥轮换：需要 EasyTier 核心支持多密钥/密钥版本
- 移动端管理 App：Web 端已做移动适配，可覆盖
- Tailscale 式中继 DERP 自建：EasyTier 有自有中继体系

---

## 官方 easytier-web 功能迁移矩阵（v2.6.4 实测 CLI + 源码行为）

easytier-web.exe = 配置服务器（core 用 `--config-server udp://host:22020/<user>` 反向接入）+ REST API（11211）+ 内嵌 Web 前端 + sqlite。以下是它有而我们已有/待做的对照。

| 官方 easytier-web 能力 | 本项目状态 | 说明 |
|---|---|---|
| 多机集中管理（core 反向接入配置服务器） | ✅ 对等（fleet） | 官方是 core 长连服务器拉配置；我们是 agent 心跳+指令，且兼容官方 core 直接跑 |
| Web 前端（内嵌 embed 构建） | ✅ 对等 | 前端打进二进制，web 端远程管理 |
| 用户注册/登录（DB 持久化） | ✅ 对等（更强） | 我们有 scrypt 账号库 + 多用户角色 + 设备会话管理 + 可选 WebDAV 多机账号同步 |
| OIDC SSO | ❌ 未做 | 需要时把 AccountStore 抽象成 IdentityProvider 接口对接 |
| 网络配置下发/启停/删除（远程） | ✅ 对等+ | start/stop_network + 本轮新增 delete_network 指令；下发时自动保留设备本地 IP/hostname |
| 设备机器名/心跳/在线状态 | ✅ 对等 | agent 45s 在线窗口 + IP/OS/版本上报 |
| Web 门户（网络令牌加入） | ✅ 对等 | 分享链接 + 网络令牌本就有 |
| 机器分组（按用户隔离设备） | ✅ 对等 | fleet agent 按账号 self-enroll 归属 |
| GeoIP 客户端位置展示 | ⛔ 不做 | 纯展示型，依赖 60MB mmdb；如需可后加 |
| Webhook 令牌校验/事件回调 | ✅ 对等（告警 webhook 已有） | 官方用于集中鉴权；我们的告警 webhook 覆盖事件通知 |
| 内部鉴权 Token（X-Internal-Auth） | ✅ 对等 | X-Auth-Token / Bearer 双头 |
| 详尽日志（trace/debug 分级落盘） | ⚠ 部分 | 核心日志有捕获；文件日志分级可从设置加 |
| 官方公共配置服务器（--config-server admin 直连官方） | ✅ 兼容 | 本项目管理的 core 完全可用官方服务器模式，互不影响 |
| 临时隧道（cloudflared）、审计日志、Prometheus 指标、流量历史、sticky-DHCP、MagicDNS hosts、防火墙清理、WebDAV 配置同步、MTU 探测、大子网助手 | 🌟 官方没有 | 本项目独有增强 |

结论：官方 easytier-web 的**远程管理核心能力已全部有对等实现**（本轮补齐 delete_network 后完整覆盖增删改启停）。剩余可做项只有 OIDC 与分级文件日志（低优先级）；GeoIP 不做。官方没有而本项目独有的 9 项增强见上表末行。
