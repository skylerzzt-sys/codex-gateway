# Phase 6：Windows Codex Gateway Profile

时间：2026-08-21（Asia/Shanghai）

## 配置

- Codex CLI：`0.147.0`
- Gateway profile：`C:\Users\Wendy\.codex\personal-gateway.config.toml`
- Profile 名：`personal-gateway`
- Provider：`personal_gateway`
- Model：`gpt-5.4`
- Base URL：`https://97.64.21.36:8443/v1`
- Wire API：`responses`
- Key 来源：用户环境变量 `CODEX_GATEWAY_API_KEY`

API key 未写入 TOML、批处理文件或本地说明记录。

## 官方回退

- 默认 `C:\Users\Wendy\.codex\config.toml` 保持 `model_provider = "openai"`
- 默认配置与变更前备份 SHA256 相同
- 备份：`C:\Users\Wendy\.codex\config.toml.bak-personal-gateway-20260821T094917`
- `codex login status`：Logged in using ChatGPT
- 不带 `--profile personal-gateway` 时继续使用官方 OpenAI

## 验证

- `--strict-config`：通过
- 实际 Provider：`personal_gateway`
- 实际 Model：`gpt-5.4`
- 临时只读 CLI 请求：成功返回 `GATEWAY_OK`
- 测试使用 `--ephemeral`，未保存会话

## 双击入口

- `D:\CodexWorkspace\codex-personal-gateway\启动个人网关Codex.bat`
- `D:\CodexWorkspace\codex-personal-gateway\启动个人网关Codex桌面版.bat`
- `D:\CodexWorkspace\codex-personal-gateway\绑定个人网关Codex桌面版.bat`
- `D:\CodexWorkspace\codex-personal-gateway\恢复官方Codex直连.bat`
- Desktop 使用可逆 App Bind 修改用户级 `config.toml`；不修改官方二进制，不增加本地常驻进程
- App Bind 前的官方配置已备份；恢复入口只恢复顶层 model/provider，保留其他后续配置修改
- 普通 `codex` 命令会跟随当前 App Bind；需要隔离测试时仍可用 `--profile personal-gateway`

## Desktop App Bind 修正

- `codex --profile personal-gateway app` 已证实只让 Desktop 启动阶段读取网关模型列表，profile 未可靠传入后续 App 请求链
- 已改为成熟实现使用的 canonical `~/.codex/config.toml` App Bind 方式
- canonical 配置已通过 `--strict-config` 与真实 gpt-5.4 Responses 请求验证；VPS 记录 `POST /v1/responses` 200
- 当前 App 不被强制关闭；重新打开后才会稳定应用新的顶层 provider
- provider 切换可能让旧会话暂时被过滤隐藏，但不会删除会话；运行恢复入口并重新打开即可恢复官方 provider 视图

## 验证边界

- Gateway CLI 已完成真实端到端验证
- Remote App Bind 已写入、通过 canonical CLI 端到端验证，并提供可逆恢复入口
- Desktop 重新启动后使用客户端 `0.148.0` 读取 Gateway 模型列表
- Desktop 新任务于 VPS 时间 `2026-08-20 22:31:19 -04:00`、`22:31:23 -04:00` 两次命中 `POST /v1/responses`，均为 HTTP 200
- Desktop Remote App Bind 真实端到端验收通过
