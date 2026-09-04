# macOS 适配

这一目录把原先依赖 BAT + PowerShell 的本地 Codex 使用层迁到 macOS，同时保留现有 VPS、Nginx、CLIProxyAPI 和双账号插件架构不变。

## 设计

- CLI 继续使用 Codex 原生 `personal-gateway` profile。
- Desktop 继续使用已经在 Windows 验证过的 canonical `~/.codex/config.toml` bind 方案；切换只修改顶层 `model` / `model_provider` 并刷新 `personal_gateway` provider block，不删除会话。
- Gateway API Key 不写入 TOML、脚本或仓库。macOS 使用 Login Keychain 保存密钥，Codex 的 provider `auth.command` 通过 `/usr/bin/security` 读取。
- 配置切换完全由 Bash + awk 完成，不经过 PowerShell，不做编码转码；中文注释和中文配置值有专门的 UTF-8 回归测试。
- `CODEX_HOME` 仍受支持；默认目录是 `~/.codex`。

## 一次性安装

先确认 `codex` 在终端可用，然后在仓库根目录执行：

```bash
bash macos/install.sh
```

安装器会：

1. 从当前 `CODEX_GATEWAY_API_KEY`、已有 Keychain 项或隐藏输入中取得 Gateway Key。
2. 将 Key 保存为 Keychain generic password：account=`codex-gateway`，service=`codex-personal-gateway`。
3. 生成 `~/.codex/personal-gateway.config.toml`；旧 profile 若存在会备份为 `.bak-macos-install`。
4. 把命令安装到 `~/.local/bin`。

如果 `~/.local/bin` 不在 PATH，按安装器提示把它加入 `~/.zshrc`。

## 日常命令

| 命令 | 用途 |
| --- | --- |
| `codex-gateway` | 使用官方 A/B 账号池的 Personal Gateway CLI |
| `codex-teamo` | 显式使用 `teamo/gpt-5.6-sol` |
| `codex-gateway-app [path]` | 绑定 Gateway 后打开 Codex Desktop |
| `codex-gateway-bind` | 只绑定 Desktop，不启动应用 |
| `codex-official` | 恢复官方 OpenAI provider |

`macos/` 目录同时提供与 Windows BAT 对应的 `.command` 双击入口。首次执行 `install.sh` 后即可使用。

Desktop bind 首次执行时，如果尚无稳定官方备份，会先保存：

```text
~/.codex/config.toml.personal-gateway-official.bak
```

若备份不存在且当前顶层 provider 已经不是 `openai`，脚本会拒绝覆盖，避免把未知第三方配置当成官方基线。

## UTF-8 回归测试

在仓库根目录运行：

```bash
bash macos/test-bind.sh
```

测试会在临时 HOME 中放入中文注释、中文 MCP 参数、旧 provider block，然后执行 Gateway → Official 往返切换。它不读取真实密钥，也不会修改真实 `~/.codex`。

## 在 Mac 上构建 VPS 插件

VPS 仍然是 AlmaLinux x86_64，因此最终产物必须是 Linux amd64 `.so`，不能直接把 macOS `.dylib` 上传过去。

Apple Silicon / Intel Mac 推荐安装：

```bash
brew install go node zig
```

前端测试与构建：

```bash
cd upstream/cpa-account-config-manager/web
npm test -- --run
npm run build
```

Go 原生测试：

```bash
cd upstream/cpa-account-config-manager
go test -p 1 ./...
```

Linux amd64 插件交叉构建：

```bash
bash tools/build-plugin.sh
```

`tools/build-plugin.sh` 现在使用仓库相对路径：Linux/WSL x86_64 继续原生构建；macOS 自动设置 `GOOS=linux`、`GOARCH=amd64`、`CGO_ENABLED=1`，并使用 Zig 作为 Linux glibc 交叉 C 编译器。产物仍是：

```text
upstream/cpa-account-config-manager/dist/cpa-account-config-manager.so
```

上传 VPS 前仍应在本地完成测试，并在服务器侧核对文件类型、SHA-256 和服务健康状态。

## 当前验证边界

本次适配已经对 shell 语法和 bind 逻辑做了隔离测试，UTF-8 往返测试通过。Keychain provider、真实 Codex Desktop 请求以及 macOS → Linux amd64 的最终 `.so` 仍需要在你的实际 Mac 上做一次端到端验收；在这一步完成前，不替换 VPS 当前生产插件。
