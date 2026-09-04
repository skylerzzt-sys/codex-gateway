# Phase 3：自定义插件隔离部署

时间：2026-08-20 23:20（Asia/Shanghai）

## 结果

- VPS：`root@97.64.21.36`
- 服务：`codex-personal-stage.service`
- 插件：`cpa-account-config-manager` `0.3.1333-personal.1`
- 状态：已注册、已启用、运行正常
- 监听：仅 `127.0.0.1:18317`

## 制品与备份

- 发布 ZIP SHA256：`42aa5d16ada857f84f21c0f731357675130964debc97650352a6b43b484bd7a5`
- 部署 SO SHA256：`06363017c60c5676f7639bf7cd0402ebfe09e5e50083e3c11d1127acf1076b61`
- 原插件 SHA256：`248ba5d187b4fe1e456b035fe6ddedeb5ecad2ef51b54baa8b38f2391fecbf4c`
- 回滚备份：`/opt/codex-personal-stage/backups/phase3-20260820T151830Z/cpa-account-config-manager.so`

## 验证

- 未授权 `/v1/models`：HTTP 401
- 已授权 `/v1/models`：HTTP 200
- 未授权 `/v0/management/plugins`：HTTP 401
- 插件 `registered=true`、`effective=true`
- 六个双账号配置字段均已注册
- systemd：`active/running`，`NRestarts=0`
- 内存：`MemoryCurrent=27,746,304`，`MemoryPeak=28,270,592` bytes
- Nginx、S-UI、value_daily：均为 active
- systemd failed units：0

## 未执行

- 未登录 OAuth 账号
- 未修改公网入口、Nginx、S-UI、firewall
- 未修改 Windows Codex 配置

