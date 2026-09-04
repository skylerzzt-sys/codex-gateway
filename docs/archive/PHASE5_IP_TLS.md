# Phase 5：公网 IPv4 TLS 与 8443 入口

时间：2026-08-21（Asia/Shanghai）

## 结果

- 公网入口：`https://97.64.21.36:8443/v1`
- TLS 证书：Let's Encrypt shortlived IP certificate
- IPv4 SAN：`97.64.21.36`
- 签发者：Let's Encrypt `YE2`
- 有效期：2026-08-21 00:10:47 UTC 至 2026-08-27 16:10:46 UTC
- Windows 系统信任链验证：通过，未使用 `-k`

## 证书

- Certbot：`5.7.0`
- 运行环境：`/opt/certbot`，Python `3.11.13`
- Production fullchain：`/etc/letsencrypt/live/97.64.21.36/fullchain.pem`
- Production private key：`/etc/letsencrypt/live/97.64.21.36/privkey.pem`
- 私钥权限：`0600 root:root`
- staging 与 production HTTP-01 均签发成功

## Nginx

- 配置：`/etc/nginx/conf.d/codex-personal-gateway.conf`
- 仅监听：`97.64.21.36:8443/tcp`
- 后端：`127.0.0.1:18317`
- `/v1`：HTTPS 健康响应
- Codex models、Responses、compact、search 与 CLI 别名：允许代理
- `/v0/management` 与其他路径：不代理
- 变更前备份：`/opt/codex-personal-stage/backups/phase5-20260821T011012Z/nginx-etc.tar.gz`

## 自动续期

- Timer：`certbot-renew.timer`，每天 00:00、12:00 检查，附加 30 分钟随机延迟
- Service：`certbot-renew.service`，`MemoryMax=256M`
- Deploy hook：`/etc/letsencrypt/renewal-hooks/deploy/10-reload-nginx.sh`
- 续期成功后仅执行：`systemctl reload nginx`
- `certbot renew --dry-run --run-deploy-hooks`：通过

## 验证

- Windows `https://97.64.21.36:8443/v1`：受信任、HTTP 200
- HTTPS `/v1/models`：HTTP 200（授权）
- HTTPS `/v1/responses`：HTTP 200，真实模型 `gpt-5.4`
- 公网 `/v0/management/plugins`：HTTP 404
- Nginx：active，master PID 未变，`NRestarts=0`
- S-UI：active，PID 未变，`NRestarts=0`
- CLIProxyAPI：active，PID 未变，`NRestarts=0`
- TCP/UDP 443：仍由 S-UI 持有

## 安装变化

- 新增 AlmaLinux 官方包：Python 3.11、pip 及其运行依赖
- 新增隔离 Certbot 5.7.0 环境
- DNF 同步升级：`sqlite`、`sqlite-devel`、`sqlite-libs` 至 AlmaLinux 当前更新版本
- 未修改、停止或重启 S-UI/Xray
- 未配置 Windows Codex provider 或 API key

