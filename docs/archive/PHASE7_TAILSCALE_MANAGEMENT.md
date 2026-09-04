# Phase 7：Tailscale 私网管理入口

时间：2026-08-21（Asia/Shanghai）

## 入口

- URL：`https://chain-dashboard.tailf45cd9.ts.net:8444/v0/resource/plugins/cpa-account-config-manager/index.html`
- 访问范围：仅当前 Tailscale tailnet
- Windows Tailscale：服务为 Automatic，已验证可访问 VPS
- SSH Tunnel：不再是日常查看额度的必要条件

## 转发链路

```text
Windows Browser
  -> Tailscale HTTPS :8444
  -> Nginx 127.0.0.1:18318
  -> CLIProxyAPI 127.0.0.1:18317
```

Nginx 配置：`/etc/nginx/conf.d/codex-management-tailnet.conf`

## 安全边界

- 公网 `https://97.64.21.36:8443/v0/management/...` 仍返回 404。
- 私网页面资源返回 200。
- 私网 Management API 无密钥返回 401。
- 私网 Management API 使用有效 Management Key 返回 200。
- Management Key 不写入 URL、本地快捷方式或项目文件。
- TCP/UDP 443 仍由 S-UI 占用，S-UI 未重启。

## 备份与回滚

备份：`/opt/codex-personal-stage/backups/phase7-bridge-20260820T225153-0400`

回滚步骤：

```bash
tailscale serve --https=8444 off
rm /etc/nginx/conf.d/codex-management-tailnet.conf
nginx -t
systemctl reload nginx
```

回滚不影响 Gateway、公网 Codex API 或原 Tailscale 8443 服务。
