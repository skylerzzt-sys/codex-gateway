# Phase 1 隔离部署记录

执行日期：2026-08-20

## 已部署

- VPS：`root@97.64.21.36`（AlmaLinux 9，x86_64）
- 目录：`/opt/codex-personal-stage`
- 服务：`codex-personal-stage.service`
- 监听：`127.0.0.1:18317`
- CLIProxyAPI：`v7.2.137`
- CPA Account Config Manager：`v0.3.1333`
- 管理面板：禁用，避免运行时自动下载
- OAuth 账号：尚未录入
- 公网入口：尚未配置

## 完整性

- `CLIProxyAPI_7.2.137_linux_amd64.tar.gz`
  - SHA-256：`ae68c776e124dbc8c8c5b86c501fc6906efa180cc5e35383adb26d05c2c91401`
- `cpa-account-config-manager_0.3.1333_linux_amd64.zip`
  - SHA-256：`79b5a5a804a1829517d2631983ec7c36e0e3a952041e26e195dcbcf4a6ef9022`

两个包均与 GitHub Release digest 和发布方 checksum 文件一致。

## 安全与资源限制

- 配置及密钥文件权限：`0600 root:root`
- Auth 和数据目录权限：`0700 root:root`
- API key 与 Management key 只在 VPS 生成，未写入本地记录或终端输出
- CPA workers：`1`
- systemd `MemoryHigh=384M`
- systemd `MemoryMax=512M`
- 管理接口仍要求密钥，且 `allow-remote=false`
- 未修改 443、S-UI、Nginx、防火墙或 Windows Codex 配置

## 验证结果

- 服务：`enabled`、`active`、`NRestarts=0`
- 监听：仅 `127.0.0.1:18317`
- 未授权 `/v1/models`：HTTP 401
- 已授权 `/v1/models`：HTTP 200
- 未授权 `/v0/management/plugins`：HTTP 401
- 插件：`registered=true`、`effective=true`、版本 `0.3.1333`
- cgroup 内存：约 26 MiB，峰值约 28 MiB
- 进程 RSS：约 67 MiB
- VPS 可用内存：约 1.03 GiB；Swap 可用约 538 MiB
- `nginx`、`s-ui`、`value_daily`：均为 active
- systemd failed units：0

## 暂未执行

- Codex OAuth A/B 登录
- 双账号角色与调度插件开发
- 5h/7d 额度及透支探测
- HTTPS 公网入口
- Windows Codex Desktop/CLI 绑定

## 回滚边界

如需撤销本阶段，先停止并禁用 `codex-personal-stage.service`，移除该独立 unit，最后再决定是否保留 `/opt/codex-personal-stage` 作为备份。未经单独授权不删除目录或密钥。
