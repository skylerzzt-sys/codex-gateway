# Phase 4：双账号接入与调度配置

时间：2026-08-21 00:10（Asia/Shanghai）

## 结果

- Codex OAuth 账号：2 个
- 账号 A：Primary
- 账号 B：Backup
- 调度模式：Auto
- 5h / 7d 真实业务额度探测：已启用
- 凭据文件：均为 `0600 root:root`
- 配置文件：`0600 root:root`

账号 ID、邮箱、设备码和 Token 均未写入本地记录。

## 配置备份

- `/opt/codex-personal-stage/backups/phase4-20260820T160951Z/config.yaml`

## 验证

- 账号管理器识别：2 个 Codex 账号
- `/v1/models`：HTTP 200
- 最小真实 `/v1/responses`：HTTP 200
- 验证模型：`gpt-5.4`
- 插件：registered、effective
- systemd：active/running，`NRestarts=0`
- 内存：`MemoryCurrent=31,367,168`，`MemoryPeak=31,928,320` bytes
- 监听：仅 `127.0.0.1:18317`
- Nginx、S-UI、value_daily：均为 active
- systemd failed units：0

## 未执行

- 未开放公网 HTTPS
- 未修改 Nginx、S-UI、firewall
- 未修改 Windows Codex 配置

