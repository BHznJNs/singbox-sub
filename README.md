# singbox-sub

单容器 VLESS + TCP + REALITY 代理（`xtls-rprx-vision`），同时提供可直接导入 Clash Verge Rev / Mihomo 的完整 YAML 订阅。仅实现单用户、单节点，不包含管理面板、Compose 或 Cloudflare Tunnel 客户端。

## 构建与启动

在项目目录构建镜像（镜像尚未发布到公共仓库）：

```bash
docker build -t singbox-sub .
```

最小启动命令（以下命令用于 VPS 的 Bash）：

```bash
docker run -d \
  --name singbox-sub \
  --restart unless-stopped \
  -p 24443:24443/tcp \
  -p 127.0.0.1:8080:8080 \
  -e SERVER_ADDR='你的VPS公网IP或域名' \
  singbox-sub
```

替换 `SERVER_ADDR` 后即可使用；无需手动创建 UUID、密钥或配置文件。VPS 防火墙/安全组需要放行代理端口 TCP 24443。

```bash
docker logs singbox-sub
```

只有代理监听和 HTTP 服务均就绪后，日志才会输出：

```text
Services ready. Subscription URL: http://localhost:8080/<自动生成的令牌>/clash.yaml
```

此处 `localhost` 是 **VPS 宿主机**，不是你运行 Clash 的电脑。订阅地址用于下载配置；代理流量直接连接 `SERVER_ADDR:SERVER_PORT`，不经过订阅 HTTP 服务。

## GitHub Actions 发布到 GHCR

工作流位于 `.github/workflows/publish-image.yml`，构建并推送 `linux/amd64` 和 `linux/arm64` 多架构镜像，复用当前 Dockerfile 中的测试与静态检查，并启用 GitHub Actions 构建缓存。

| 触发方式 | 发布标签 |
| --- | --- |
| 推送到 `main` | `latest`、`sha-<完整提交 SHA>` |
| 推送 `v*` 标签，例如 `v1.0.0` | `v1.0.0`、`sha-<完整提交 SHA>`，不更新 `latest` |
| Actions 页面手动运行 | 始终发布 SHA 标签；选择 `main` 时同时发布 `latest`，选择版本标签时同时发布对应标签 |

镜像名称自动取小写仓库名，当前为 `ghcr.io/bhznjns/singbox-sub`。工作流使用 GitHub 自动提供的 `GITHUB_TOKEN`，仅授予 `contents: read` 和 `packages: write`，无需额外配置 PAT 或仓库 Secret。所用 Actions 固定到提交 SHA。

首次使用：

1. 将工作流提交并推送到 GitHub 的 `main`，或在 Actions 页面选择 **Publish image to GHCR → Run workflow**。
2. 确认仓库允许 GitHub Actions 运行、允许使用这些 Actions，且组织策略没有禁止 `packages: write`。若同名 GHCR 包已存在，在包的 **Package settings → Manage Actions access** 中授予本仓库写入权限。
3. 发布成功后，在个人或组织的 **Packages → singbox-sub → Package settings → Change visibility** 中按需设为 **Public**，否则匿名用户不能直接拉取。仓库公开不代表新建的 GHCR 包自动公开。

镜像成功发布并设为公开后，可直接运行，无需本地构建：

```bash
docker run -d \
  --name singbox-sub \
  --restart unless-stopped \
  -p 24443:24443/tcp \
  -p 127.0.0.1:8080:8080 \
  -e SERVER_ADDR='你的VPS公网IP或域名' \
  ghcr.io/bhznjns/singbox-sub:latest
```

需要固定版本时，将 `latest` 替换为已发布的版本标签或 SHA 标签。此工作流只在推送或手动触发时发布，不对 Pull Request 自动发布镜像；本地添加工作流并不代表镜像已经发布。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVER_ADDR` | 必填 | VPS 公网 IPv4、IPv6 或 ASCII 域名，不带协议、端口或路径；IPv6 不带方括号 |
| `SERVER_PORT` | `24443` | sing-box 监听端口，同时写入订阅；范围 1–65535，不能与订阅端口 8080 冲突 |
| `REALITY_SERVER_NAME` | `www.microsoft.com` | REALITY 握手目标及客户端 SNI，目标端口为 443；不是 VPS 地址或订阅域名 |
| `SUB_BASE_URL` | `http://localhost:8080` | 仅影响日志中的订阅 URL；允许 HTTP(S) 基址和路径前缀，不允许用户凭据、查询参数或 fragment |

可选变量未设置或为空时使用默认值。域名需提前解析至正确地址；程序不会申请域名、证书、配置 HTTPS 或自动建立隧道。

自定义代理端口时必须保持宿主机和容器端口一致，例如同时使用 `-e SERVER_PORT=34443` 与 `-p 34443:34443/tcp`。建议使用非特权端口。

默认 REALITY 目标已在开发环境验证支持 TLS 1.3 和 HTTP/2；实际可用性取决于 VPS 到该目标的网络和目标后续变更。目标不通时可通过环境变量更换。运行时不会把目标临时网络故障当作身份损坏，也不会重新生成身份。

## 通过已有 Cloudflare Tunnel 发布订阅

将已有 Tunnel 的订阅域名转发到 VPS 宿主机 `http://localhost:8080`，并在上述启动命令的镜像名之前增加：

```bash
-e SUB_BASE_URL='https://sub.example.com'
```

日志将显示：

```text
https://sub.example.com/<自动生成的令牌>/clash.yaml
```

将完整 URL 导入 Clash Verge Rev 的订阅配置即可。若配置了基址路径前缀，反向代理需剥离该前缀再转发。若 Tunnel 客户端也运行在另一个容器中，该容器的 `localhost` 不代表宿主机，需要自行配置其访问宿主机的方式。

不要将 REALITY 的原始 TCP 代理端口当作订阅域名的 HTTP 流量转发；这是两条不同的连接。请将令牌 URL 视为凭据，不要公开；推荐用 HTTPS 发布订阅，并保持 8080 仅绑定宿主机回环地址。HTTP 不会返回目录列表、私钥或内部状态，只允许精确的 `/<token>/clash.yaml` 路径（GET/HEAD）。

## 身份保存

程序在 `/data/identity/state.json` 保存 UUID、REALITY 密钥对、Short ID 和 256 位安全随机令牌。私钥不写入订阅，不主动输出到日志；身份和生成文件使用仅所有者可读写的权限。`/data/sing-box.json` 与 `/data/clash.yaml` 每次启动从同一份身份生成，启动前执行 `sing-box check`。

- 同一容器停止、启动、重启：身份与令牌不变；环境变量不变时订阅 URL 不变。
- 无数据卷删除再创建容器：生成新身份和令牌，需要重新导入。
- 更新或重建容器时需要保留身份，在启动命令的镜像名之前增加：

  ```bash
  -v singbox-sub-data:/data
  ```

  重建时复用同一命名卷即可，无需维护其中的文件。不要让多个容器同时使用同一数据卷。

已有身份缺失、不完整、JSON 损坏、字段无效或密钥对不匹配时，程序明确报错退出，不自动覆盖。身份目录也充当首次初始化标记；初始化被中断时保留标记并拒绝下次静默重建。若整个身份目录丢失但 `/data` 中仍有生成文件，也会拒绝重建。若整个 `/data` 被清空，则无法区分它与全新数据目录；请保护数据卷并备份。

正常停止时主程序关闭 HTTP 并停止 sing-box；代理或 HTTP 服务意外退出会使容器失败退出，供 Docker 重启策略接管。参数错误、配置检查失败、端口占用等不会输出成功启动消息。程序固定监督独立 sing-box 进程，镜像内不需要 shell 启动脚本。

## 订阅内容

订阅包含一个 REALITY 节点、默认选择该节点的 `PROXY` 选择组、可选 `DIRECT`、DNS 与基本路由。私网地址直连，其余流量走 `PROXY`；不依赖额外规则集或 GeoIP/GeoSite 文件。DNS 使用公共 DoH 服务，不包含复杂的地区分流规则。启用了 xudp；服务端 Docker 仅需映射 TCP 代理端口。

## 开发与验证

主程序仅依赖 Go 标准库（Go 1.24+）；镜像使用 Go 多阶段构建，固定 sing-box `1.14.4`，不在启动时下载二进制。

```bash
go test ./...
go vet ./...
```

可选真实内核集成测试（提供已下载的 sing-box 和 Mihomo 可执行文件绝对路径）：

```bash
SING_BOX_TEST_BINARY=/path/to/sing-box MIHOMO_TEST_BINARY=/path/to/mihomo go test -v ./...
```

未提供可执行文件时集成测试明确跳过。真实内核测试会临时监听本机端口；运行前确保 8080 空闲。跨平台构建可使用 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .`，Docker BuildKit 也支持按目标架构构建。

### 本次验证记录（2026-10-24）

| 项目 | 结果 |
| --- | --- |
| Go 全套测试（含真实内核集成） | 通过，语句覆盖率 83.6% |
| `go vet ./...` | 通过 |
| Linux amd64 / arm64 静态交叉编译 | 通过 |
| 默认目标 `www.microsoft.com:443` | 已验证证书、TLS 1.3、ALPN h2 |
| sing-box 1.14.4 / Mihomo 1.19.33 | 服务端配置检查、原始订阅检查通过；本机通过 REALITY/Vision 链路完成 HTTP 请求 |
| 重启与故障测试 | 相同数据目录重启身份和 URL 不变；损坏身份拒绝覆盖；代理/HTTP 启动失败、运行中退出均能传播失败并清理代理进程 |
| Docker 镜像构建与容器/命名卷验收 | 未执行：本机 Docker Desktop Linux 引擎未运行；已核实固定 sing-box 镜像标签和 amd64/arm64 清单存在 |
| VPS 公网及 Clash Verge Rev GUI 导入 | 未执行：无 VPS/GUI 验收环境 |

集成测试先检查未修改的订阅，再为本机回环测试调整客户端本地监听端口并将路由改为全部走代理，避免私网直连规则绕过待测链路。测试下载的内核放在被 Git 和 Docker 构建上下文忽略的 `.tools/` 中，不属于运行时依赖。

公网验收应在 VPS 上执行：构建启动后从日志获取 URL，下载并导入 Clash Verge Rev，确认可选择节点并完成实际代理访问；然后分别验证原容器重启、命名卷重建、错误令牌/私钥路径的 404、端口占用与损坏身份导致的失败退出。本机测试不能替代 VPS 的公网连通性和 Clash Verge Rev GUI 验收。
