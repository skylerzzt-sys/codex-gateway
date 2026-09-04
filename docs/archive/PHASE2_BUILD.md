# Phase 2 本地构建记录

执行日期：2026-08-20

## 结果

- 分支：`codex/personal-dual-account-gateway`
- 基线：CPA Account Config Manager `v0.3.1333`
- 自定义版本：`0.3.1333-personal.1`
- 构建平台：Linux amd64
- Go：`1.26.7`，项目私有工具链
- Node：复用 Windows `24.15.0`
- 未部署 VPS，未录入 OAuth

## 双账号调度

- 仅处理 `codex` provider，其他 provider 返回未处理。
- 只允许配置的 A/B Auth ID；不读取或保存 Token、Auth JSON。
- `auto`：Primary 可用时优先；Primary 被宿主过滤后选择 Backup。
- `force_a` / `force_b`：目标不在宿主候选中时 fail-hard，不溢出到另一账号。
- 角色：`primary` / `backup` / `disabled`。
- 候选已经由 CLIProxyAPI 完成 disabled、tried、cooldown、quota、模型兼容和 priority 过滤。

CLIProxyAPI 原生插件配置字段：

```yaml
gateway_account_a_id: "<AUTH_ID_A>"
gateway_account_b_id: "<AUTH_ID_B>"
gateway_role_a: primary
gateway_role_b: backup
gateway_mode: auto
gateway_overdraft_enabled: true
```

A/B 应保持相同的 CLIProxyAPI 原生 Priority。

## Overdraft

- 5h 与 7d 独立状态：`normal/pending/passed/failed/inconclusive/recovered`。
- `used_percent >= 95` 进入 pending。
- 使用真实 Codex Responses 请求注入一个有界 no-op tool pair。
- 每个账号、每个窗口、每个 cycle 最多 claim 一次。
- 同一真实请求可同时验证 5h 与 7d。
- 只有明确 subscription quota 证据进入 failed。
- 未知 429、5xx、超时、认证或网络失败进入 inconclusive。
- 同 cycle 的 failed 为终态；迟到成功和旧 cycle completion 不能覆盖。
- 状态只持久化额度、周期、证据种类和 claim 标志，不持久化原始 Body、Header、API key 或 Token。
- 主文件和备份均损坏时 fail-closed，不再注入。
- Host schema 不支持 request lifecycle 时自动禁用 overdraft。

## 验证

- `go test -p 1 ./...`：通过。
- `go test -race -p 1 ./internal/manager`：通过。
- `go vet ./...`：通过。
- `npm run typecheck`：通过。
- `npm run build`：通过，单文件 WebUI 约 976 kB。
- 前端基线测试：217/218；唯一失败是上游测试在 Windows 反斜杠路径下未正确排除 i18n 目录，本阶段未改该无关测试。
- `git diff --check`：通过。
- `.so`：ELF 64-bit x86-64，Go `1.26.7`，仅动态依赖 glibc。

## 产物

- `upstream/cpa-account-config-manager/dist/cpa-account-config-manager.so`
  - 大小：15,857,472 bytes
  - SHA-256：`06363017C60C5676F7639BF7CD0402EBFE09E5E50083E3C11D1127ACF1076B61`
- `upstream/cpa-account-config-manager/dist/release/cpa-account-config-manager_0.3.1333-personal.1_linux_amd64.zip`
  - 大小：7,572,784 bytes
  - SHA-256：`42AA5D16ADA857F84F21C0F731357675130964DEBC97650352A6B43B484BD7A5`
- ZIP 内仅包含 `cpa-account-config-manager-v0.3.1333-personal.1.so`。

## 未执行

- 未上传或替换 VPS 插件。
- 未重启 Phase 1 服务。
- 未修改 VPS 配置、443、Nginx、S-UI 或防火墙。
- 未进行 OAuth A/B 登录。
- 未修改 Windows Codex Desktop/CLI 配置。
