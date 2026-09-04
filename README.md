# Personal Dual-Account Codex Gateway

这是 Wendy 的个人双账号 Codex 网关。Codex CLI/Desktop 通过 VPS 上的 CLIProxyAPI 访问两个 Codex OAuth 账号；公网 API、私网管理面和官方直连回退彼此隔离。

本文档是项目唯一的当前说明。历史设计与阶段记录位于 `docs/archive/`，仅用于追溯，不再作为操作依据。

## 当前状态

最后验证：2026-08-24（Asia/Shanghai）

| 项目 | 当前值 |
| --- | --- |
| VPS | `97.64.21.36`，AlmaLinux 9，x86_64 |
| 项目目录 | `/opt/codex-personal-stage` |
| systemd 服务 | `codex-personal-stage.service` |
| CLIProxyAPI | `v7.2.137-personal.1`，仅监听 `127.0.0.1:18317` |
| 自定义插件 | `cpa-account-config-manager 0.3.1333-personal.1` |
| 活动插件 | `/opt/codex-personal-stage/plugins/linux/amd64/cpa-account-config-manager.so` |
| 公网 API | `https://97.64.21.36:8443/v1` |
| 私网管理 | `https://chain-dashboard.tailf45cd9.ts.net:8444/v0/resource/plugins/cpa-account-config-manager/index.html` |
| 账号 | 2 个 Codex OAuth 账号；账号 ID、邮箱和 Token 不写入本地文档 |

最新部署 SHA-256：

```text
CLIProxyAPI  164b00a1c4733d0178164eb6098bc7d95bfe96aa0d598657d05e01547f86c12f
Plugin      457925a4d78e8ea16a7b22cc5e1b2c72ab5b7f69b9cef0cb12f5ede4cdc9f1fd
```

最新验收结果：前端测试 220/220、插件 Go 全部包通过、宿主认证调度包完整通过；公网 API 与私网管理页均为 HTTP 200；服务 `active/running`、`NRestarts=0`。选择 A 后的无历史最小请求返回 `ROUTE_A_OK`，A 成功请求从 0 增至 1，B 未增加。Nginx、S-UI/Xray 保持运行，TCP/UDP 443 未被本项目接管。

## 日常使用

根目录提供以下 Windows 入口：

| 文件 | 用途 |
| --- | --- |
| `启动个人网关Codex.bat` | 使用 `personal-gateway` profile 和官方 A/B 账号池启动 Codex CLI |
| `启动中转站Codex.bat` | 使用同一网关显式选择 `teamo/gpt-5.6-sol` 中转模型 |
| `启动个人网关Codex桌面版.bat` | 绑定网关后启动 Codex Desktop |
| `绑定个人网关Codex桌面版.bat` | 只绑定 Desktop，不启动应用 |
| `恢复官方Codex直连.bat` | 恢复官方 OpenAI provider |
| `查看双账号额度.url` | 打开 Tailscale 私网管理页 |

Desktop 绑定或恢复后需要重新打开 Codex 才会稳定生效。配置切换由 `codex-app-bind.ps1` 原子执行，不修改官方二进制，也不删除会话。

Gateway API Key 只从用户级环境变量 `CODEX_GATEWAY_API_KEY` 读取，不得写入 BAT、TOML、项目文件或文档。Management Key 只在进入私网管理页后手动使用，不得写入 URL 或快捷方式。


## 双账号选择

管理页账号表格把 OAuth 账号和托管通道放在同一列表中选择：

- `自动（OAuth）`：Primary 可用时使用 Primary；不可用时使用 Backup。
- `账号 A/B`：后续请求固定使用所选 OAuth 账号。
- 强制账号不可用时直接返回明确错误，不会静默溢出到另一个账号。
- 所有选择持续生效，直到重新选择；切换通道不修改 A/B 绑定、角色或 OAuth 文件。

插件调度会收到所有已通过状态、额度、模型和重试过滤的 Codex 候选，不再被原生 Priority 提前裁剪；插件未处理的其他路由仍保留 CLIProxyAPI 原生 Priority 语义。5h/7d 额度探测只保存状态、周期和证据类型，不保存请求正文、Token 或密钥。

## TeamoRouter 中转 MVP

默认模型名（例如 `gpt-5.4`）继续进入官方 A/B 账号池。只有显式使用 `teamo/` 前缀时才进入 TeamoRouter：

```text
teamo/gpt-5.6-sol
teamo/gpt-5.6-terra
teamo/gpt-5.6-luna
```

这版不提供官方账号失败后自动转中转站，避免意外计费和会话中途换模型。VPS 上的 TeamoRouter Key 只写入权限受限的生产配置，不写入仓库、Windows 启动脚本或浏览器。首次启用时，在 VPS 环境中临时提供 `TEAMOROUTER_API_KEY`，然后运行：

```bash
python3 /opt/codex-personal-stage/deploy/merge-teamorouter-config.py /opt/codex-personal-stage/config.yaml
```

脚本会备份并原子更新现有配置，保留官方账号、Gateway Key、插件和其他 provider。配置完成后可用 `启动中转站Codex.bat` 启动中转模型；原 `启动个人网关Codex.bat` 行为不变。

接口依据 [TeamoRouter API 接入文档](https://teamorouter.com/zh/docs/api-integration)：OpenAI Base URL 为 `https://api.teamorouter.cn/v1`，GPT 系列支持 `/v1/responses`。

## 网络与安全边界

```text
Codex CLI/Desktop
  -> HTTPS 97.64.21.36:8443
  -> Nginx
  -> CLIProxyAPI 127.0.0.1:18317

Windows Browser + Tailscale
  -> HTTPS chain-dashboard.tailf45cd9.ts.net:8444
  -> Nginx 127.0.0.1:18318
  -> CLIProxyAPI 127.0.0.1:18317
```

- 公网入口只代理 Codex 所需 API；公网 `/v0/management` 返回 404 是预期行为。
- 私网管理 API 仍要求 Management Key；无密钥返回 401。
- CLIProxyAPI 管理配置保持 `allow-remote: false`。
- TCP/UDP 443 继续由 S-UI/Xray 使用；本项目 Nginx 仅使用 TCP 8443。
- Auth JSON、OAuth Token、API Key、Management Key、证书私钥和 VPS 配置值均不进入仓库。

## TLS 与自动续期

- 证书：Let's Encrypt short-lived IPv4 certificate。
- Certbot：`5.7.0`，隔离环境 `/opt/certbot`。
- 证书链：`/etc/letsencrypt/live/97.64.21.36/fullchain.pem`。
- 私钥：`/etc/letsencrypt/live/97.64.21.36/privkey.pem`，权限 `0600 root:root`。
- 自动续期：`certbot-renew.timer` 每天检查两次。
- 续期成功后只执行 `systemctl reload nginx`。

Windows 已验证 `https://97.64.21.36:8443/v1` 可由系统信任链正常验证，不需要 `-k`。

## 项目结构

```text
codex-personal-gateway/
├─ README.md                   当前唯一说明
├─ docs/archive/               历史阶段记录，不作为当前操作指南
├─ deploy/
│  ├─ certbot/                 续期 service、timer 与 deploy hook
│  ├─ nginx/                   公网 API 与 Tailscale 管理入口配置
│  ├─ codex-personal-stage.service
│  ├─ config.yaml.template
│  └─ install/render/verify 脚本
├─ tools/
│  ├─ build-plugin.sh          当前 Linux amd64 插件构建入口
│  └─ install-toolchains.sh    首次搭建脚本，会联网下载，不用于日常部署
├─ upstream/
│  ├─ CLIProxyAPI/             网关宿主源码
│  ├─ cpa-account-config-manager/ 当前自定义插件源码
│  ├─ codex-multi-auth/        参考实现，不部署
│  ├─ codex-lb/                参考实现，不部署
│  └─ sub2api-overdraft/       状态机参考，不复制其 LGPL 代码
├─ .toolchains/                项目私有 Go 工具链
└─ .cache/                     Go 构建缓存
```

`.toolchains`、`.cache`、`web/node_modules` 和构建产物均为当前本地构建所需，不属于可随意清理的过时文件。

## 构建与验证

不在 2 GB VPS 上编译。前端和插件均在 Windows/WSL 本地串行构建，Go 使用 `-p 1` 限制并发。

前端：

```powershell
Set-Location -LiteralPath 'D:\CodexWorkspace\codex-personal-gateway\upstream\cpa-account-config-manager\web'
npm test -- --run
npm run build
```

Go：

```powershell
wsl.exe -d Ubuntu --cd /mnt/d/CodexWorkspace/codex-personal-gateway/upstream/cpa-account-config-manager -- env GOMODCACHE=/mnt/d/CodexWorkspace/codex-personal-gateway/.cache/go-mod GOCACHE=/mnt/d/CodexWorkspace/codex-personal-gateway/.cache/go-build GOPATH=/mnt/d/CodexWorkspace/codex-personal-gateway/.cache/go-path /mnt/d/CodexWorkspace/codex-personal-gateway/.toolchains/go-1.26.7/bin/go test -p 1 ./...
wsl.exe -d Ubuntu -- bash /mnt/d/CodexWorkspace/codex-personal-gateway/tools/build-plugin.sh
```

产物：

```text
upstream/cpa-account-config-manager/dist/cpa-account-config-manager.so
```

`tools/install-toolchains.sh` 会联网下载依赖，只有在工具链缺失且获得明确授权时才可执行。

## 部署与运维

部署新插件时只替换活动 `.so`，不覆盖 `/opt/codex-personal-stage/config.yaml`、Auth、`data/`、日志或其他运行数据。固定流程：

1. 本地测试和构建全部通过。
2. 备份当前活动插件。
3. 上传到 VPS 临时文件并核对 SHA-256。
4. 以 `root:root 0755` 替换活动插件。
5. 只重启 `codex-personal-stage.service`。
6. 检查服务状态、公网 API、私网管理页和新 UI 标记。

常用只读检查：

```bash
systemctl status codex-personal-stage.service --no-pager
systemctl show codex-personal-stage.service -p ActiveState -p SubState -p NRestarts -p MemoryCurrent
curl -I https://97.64.21.36:8443/v1
```

Nginx 配置变更必须先执行 `nginx -t`，通过后只 reload。除非任务明确涉及其自身配置，否则不得停止或重启 S-UI/Xray。

## 备份与回滚

- 当前主程序与插件回滚备份：`/opt/codex-personal-stage/backups/phase9-routing-20260824T231700CST/`。
- 公网 TLS/Nginx 初始备份：`/opt/codex-personal-stage/backups/phase5-20260821T011012Z/nginx-etc.tar.gz`。
- Tailscale 管理桥接备份：`/opt/codex-personal-stage/backups/phase7-bridge-20260820T225153-0400`。
- Windows 官方 Codex 稳定备份：`C:\Users\Wendy\.codex\config.toml.personal-gateway-official.bak`。

插件回滚时先校验备份存在和哈希，再恢复活动 `.so`，然后只重启 `codex-personal-stage.service` 并完成健康检查。不要删除 VPS 项目目录、Auth、配置或密钥。
