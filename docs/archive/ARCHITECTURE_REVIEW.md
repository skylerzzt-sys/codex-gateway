# 个人双账号 Codex Gateway：源码与架构审查

日期：2026-08-20  
状态：授权阶段 1，未编码、未部署、未安装依赖

## 1. 审查范围

已在 `upstream/` 固定五个完整源码树，并对插件 ABI、Codex OAuth、Responses 路由、账号选择、错误冷却、quota、overdraft、Desktop App Bind、安全存储和测试实现做全仓检索与关键路径深读。

| 仓库 | 分支 | 固定提交 | 许可证 |
| --- | --- | --- | --- |
| CLIProxyAPI | `main` | `85d2faddd17e6f4f8675a84ee28b131f702e8eaa` | MIT |
| cpa-account-config-manager | `main` | `7d28d3fa1d82d64f1556f2c96c9e2e8a00a9725c` | MIT |
| sub2api-overdraft | `codex-overdraft` | `73ea828a5ffbaef4d39b1162f4c7503511fcc2da` | LGPL-3.0-or-later |
| codex-multi-auth | `main` | `fcca464294fd86596324cc5aea85997e1190845c` | MIT |
| codex-lb | `main` | `eab71553aee660fdb31122e9feb61a0e6c367904` | MIT |

源码规模约 8,800 个文件，其中 Go 3,700+、TypeScript/TSX 1,500+、Python 900+，测试文件约 3,900 个。

## 2. 最终最小架构

```text
Official Codex Desktop / CLI
        │ HTTPS + Gateway API Key
        ▼
公网 TLS 入口（与现有 S-UI 共存，方案待实测选择）
        │ 仅转发允许的 Codex API 路径
        ▼
127.0.0.1:8317
CLIProxyAPI 单进程
  └─ personal-dual-manager.so
      ├─ scheduler
      ├─ request_interceptor
      ├─ request_lifecycle_plugin
      ├─ usage_plugin
      ├─ management_api
      └─ data/state.json
        │
        ├─ Account A OAuth（Token 仅归 CLIProxyAPI）
        └─ Account B OAuth（Token 仅归 CLIProxyAPI）
```

不引入 PostgreSQL、Redis、Docker、独立账号服务或常驻 Windows bridge。插件与 CLIProxyAPI 同进程，插件状态只保存 Auth ID、角色、路由模式、quota 快照、reset time、overdraft 状态和必要的请求关联信息。

## 3. CLIProxyAPI 已提供的成熟能力

无需重写代理核心：

- 插件 ABI 已支持 `scheduler.pick`、请求前/选号后拦截、终态回调、usage observer、管理页面及 Host Auth 回调。
- Scheduler 候选已经过 Disabled、模型兼容、quota/cooldown、已尝试账号等宿主过滤；插件只需在候选中表达 Primary/Backup/Force 语义。
- 同一个请求 Metadata 同时交给 lifecycle tracker 和执行 Options；选号后宿主写入 `selected_auth_id`、`selected_auth_index`。终态回调因此可以关联 `RequestID → 实际 Auth ID → 成功/失败`。
- UsageRecord 提供 Auth ID、Auth Index、失败状态码、失败正文和响应头，可用于 quota 分类与 5h/7d 快照。
- `/v1` 路由统一使用 API Key 中间件。
- 当前 Codex 所需核心路由包括：
  - `GET /v1/models`
  - `GET|POST /v1/responses`
  - `POST /v1/responses/compact`
  - 可能使用 `POST /v1/alpha/search`
  - CLI 直连别名位于 `/backend-api/codex/*`

正式公网入口只允许 Phase 1 实测确实使用的路径，不代理 `/v0/management`、`/management.html`、OAuth 回调或其他 provider API。

## 4. 双账号调度设计

插件状态：

```text
role: Primary | Backup | Disabled
mode: Auto | ForceA | ForceB
```

### Auto

1. 从 CLIProxyAPI 已过滤的候选中选择 Primary。
2. Primary 不在候选时选择 Backup。
3. 同一请求内 Primary 出现临时失败时，宿主会把它加入 `tried`；下一次 scheduler 调用可临时选择 Backup，但不修改 Primary 角色。
4. Primary 恢复并重新进入候选后，下一次新请求自动回到 Primary。

这等价于 codex-multi-auth 的 Sequential/drain-first 语义，同时复用 CLIProxyAPI 的候选过滤、重试和流式执行。

### Force A / Force B

- 指定账号在候选中：只选择该账号。
- 指定账号不可用而另一个账号可用：返回明确错误，不溢出到另一个账号。
- 两个账号都不可用：由宿主返回 pool unavailable；仍不会暗中切换。

### Disabled

插件始终排除 Disabled 角色。为避免插件临时停用后该账号被宿主原生路由选中，最终实现应把 Disabled 同步到 CLIProxyAPI 原生 disabled 字段；更新通过 Management PATCH 完成，不读取或重写 Token。

## 5. Overdraft 状态机

每个账号的 5h 与 7d 独立存储：

```text
normal → pending → passed
                 → failed
                 → inconclusive
failed/passed/inconclusive → recovered → normal
```

每个窗口至少保存：

```text
status
cycle_key
used_percent
reset_at
evidence_kind
evidence_at
probe_claimed
reason_code
```

规则：

- `<95%`：`normal`。
- `>=95%` 且 reset 尚未到达：`pending`，选号后为该账号的真实 Codex 请求注入 no-op tool call/output pair。
- 只有携带该注入的真实请求完成成功，才能写 `passed`。
- 只有明确 subscription quota exhausted 证据才写对应窗口 `failed`。
- timeout、连接错误、5xx、认证失败、未知/非额度型 429 一律 `inconclusive`，不写 quota failed。
- 没有真实业务证据时，使用原子 claim 保证一个 `cycle_key` 最多一次主动 probe。
- reset 后不靠定时暴力探测；由下一次真实请求或手动 quota refresh 验证，成功后 `recovered`，再回 `normal`。
- 当前周期的 `failed` 为终态；并发较晚到达的成功不能覆盖已确认失败。新周期使用新 `cycle_key`。

CPA Manager 现有请求注入代码可以复用；其固定 5 次 probe 必须删除。sub2api-overdraft 的状态行为只作为规范参考，因 LGPL 许可证不直接复制其 Go 代码。

## 6. 429 与 CLIProxyAPI 原生冷却

CLIProxyAPI 默认会把普通 429 写入 quota cooldown，这与“未知 429 必须 inconclusive”冲突。

可复用其原生 `request-scoped-errors`：为两个 OAuth Auth Metadata 添加一个仅匹配 429 的通用规则，例如正则匹配所有正文并使用 `continue`（重试其他候选但不冷却当前账号）。随后由插件根据 Usage failure body/headers 独立判定：

- 明确 quota：插件窗口 `failed`，后续 scheduler 排除到 reset。
- 其他 429：`inconclusive`，下一次请求仍可尝试 Primary。

该方案避免在失败后调用 `reset-quota` 产生竞态，也不需要改 CLIProxyAPI 核心。Phase 1 必须用真实 429 fixture 和两个账号验证规则确实适用于 OAuth Auth Metadata，并确认 token refresh/relogin 会保留该字段。

## 7. Token 与管理安全边界

- `host.auth.list` 已提供 ID、Auth Index、邮箱、套餐、优先级、状态等安全展示字段；个人插件的列表页无需调用 `host.auth.get` 读取物理 Auth JSON。
- 角色、Disabled 和 request-scoped 规则使用 CLIProxyAPI Management PATCH 更新；不得将物理 JSON、Authorization、Cookie 或 Management Key 写入插件状态。
- CPA Manager 中批量导入导出、凭据转换、完整 Auth JSON 编辑、Agent Identity、PAT、通知与自动删除模块全部移除。
- Management API 保持 `allow-remote: false` 且要求管理密钥；只通过 SSH Tunnel 访问。
- 插件资源页面只包含静态 UI；所有数据和写操作继续走宿主认证后的 Management route。

## 8. Desktop / CLI 接入与会话兼容

首选直接配置用户级 custom provider：

```toml
model_provider = "personal"

[model_providers.personal]
base_url = "https://gateway.example/v1"
env_key = "CODEX_GATEWAY_API_KEY"
wire_api = "responses"
```

CLIProxyAPI 的 Codex executor 会在上游请求前删除 `previous_response_id`，降低跨 OAuth 账号 continuation 绑定风险；但 prompt cache、加密 reasoning、文件 ID、工具调用 continuation 和 Desktop resume 仍须真机验证。

`official` 回退通过原子恢复 `model_provider = "openai"` 完成。借鉴 codex-multi-auth 的备份、哈希校验、解绑和孤儿配置恢复逻辑，不 patch 官方二进制。

## 9. VPS 只读审计结果

- AlmaLinux 9.7，3 vCPU，2.0 GiB RAM，约 1.1 GiB available。
- Swap 544 MiB，仅使用约 6 MiB。
- 根盘 39 GiB，剩余约 28 GiB。
- 无 failed systemd units。
- `sshd`、`nginx`、`s-ui`、`value_daily` active；`chain-dashboard` 当前 inactive，本项目不处理。
- `s-ui` 占用公网 TCP/UDP 443；Nginx 仅监听 80。
- RSS 约：S-UI 112 MiB、Tailscale 98 MiB、value_daily 26 MiB、Nginx worker 合计约 15 MiB。
- VPS 有 Git/GCC/Make，无 Go。

结论：CLIProxyAPI 必须继续采用低进程数和低后台活动设计。正式 HTTPS 入口不能直接让 Nginx 接管 443；需另行评估：

1. 在现有 443 前端按 SNI/回落复用；
2. 使用独立 HTTPS 端口；
3. 使用独立 IP/入口。

任何 S-UI、Nginx 443、firewall 或证书调整都属于下一阶段高风险操作，必须单独授权。

## 10. 当前验证结果与构建阻塞

已完成、无写入副作用：

- 五个 Git 工作树均干净，提交与表格一致。
- codex-multi-auth `scripts/*.js`：16 个文件通过 `node --check`。
- Python 源码：955 个文件通过 Python 3.14 AST 解析。
- TOML：20 个文件通过 `tomllib` 解析。

尚未执行：

- CLIProxyAPI / CPA Manager Go build、go test：Windows、WSL、VPS 均没有 Go。
- CPA Manager / codex-multi-auth TypeScript build/test：仓库没有 `node_modules`。
- codex-lb Python tests：依赖环境未安装，且它不进入最终运行架构。

安装或下载 Go、npm、Python 依赖需要下一次明确授权。建议只为目标宿主和目标插件建立最小构建环境，不构建 sub2api-overdraft 或 codex-lb 全栈。

## 11. 下一阶段建议

下一阶段先做 Phase 1 隔离验证，不碰公网 443：

1. 授权下载固定版本的 CLIProxyAPI Linux amd64 发布包和 CPA Manager Linux amd64 插件发布包。
2. 在 VPS 创建独立测试目录与 systemd 服务，仅监听 `127.0.0.1` 的未占用端口。
3. 通过 SSH Tunnel 完成 Account A/B OAuth。
4. 用 CLI 先验证 models、Responses、stream、tool call、compact、账号切换和 quota headers。
5. 再以备份后的临时 Codex provider 配置验证 Desktop；结束后恢复 OpenAI。
6. Phase 1 通过后，再开始裁剪插件和建立最小构建工具链。

