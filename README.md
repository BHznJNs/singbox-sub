# singbox-sub

单容器 VLESS + TCP + REALITY 代理（`xtls-rprx-vision`），提供可直接导入 Clash Verge Rev / Mihomo 的完整 YAML 订阅。仅支持单用户、单节点，无需手动创建 UUID、密钥或配置文件。不包含管理面板或 Cloudflare Tunnel 客户端。

## 快速开始

在 VPS 上安装并启动 Docker，防火墙/安全组放行 TCP 24443。镜像发布并公开后，可使用以下命令（用于 Bash）：

```bash
docker run -d \
  --name singbox-sub \
  --restart unless-stopped \
  -p 24443:24443/tcp \
  -p 127.0.0.1:8080:8080 \
  -e SERVER_ADDR='你的VPS公网IP或域名' \
  ghcr.io/bhznjns/singbox-sub:latest
```

替换 `SERVER_ADDR` 即可。需要固定版本时，将 `latest` 换成已发布的版本标签或 `sha-<完整提交 SHA>`。若镜像尚未发布或无法匿名拉取，可参照[源码构建说明](docs/development.md#源码构建)自行构建，并将命令中的镜像名替换为 `singbox-sub`。

### 获取订阅

```bash
docker logs singbox-sub
```

代理与订阅服务就绪后，日志会输出：

```text
Services ready. Subscription URL: http://localhost:8080/<自动生成的令牌>/clash.yaml
```

此处 `localhost` 是 **VPS 宿主机**，不是运行 Clash 的电脑。要在客户端导入 URL，需先通过已有 HTTPS 反向代理或下文的 Cloudflare Tunnel 发布订阅，再将完整 URL 导入 Clash Verge Rev，选择 `PROXY` 组中的 `VLESS-REALITY` 节点。

订阅 URL 只用于下载配置；实际代理流量直接连接 `SERVER_ADDR:SERVER_PORT`，不经过订阅 HTTP 服务。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVER_ADDR` | 必填 | VPS 公网 IPv4、IPv6 或 ASCII 域名，不带协议、端口或路径；IPv6 不带方括号 |
| `SERVER_PORT` | `24443` | 代理监听端口，同时写入订阅；范围 1–65535，不能使用订阅服务占用的 8080 |
| `REALITY_SERVER_NAME` | `www.microsoft.com` | REALITY 握手目标及客户端 SNI，目标端口为 443；不是 VPS 地址或订阅域名 |
| `SUB_BASE_URL` | `http://localhost:8080` | 仅影响日志中的订阅 URL；允许 HTTP(S) 基址和路径前缀，不允许用户凭据、查询参数或 fragment |

可选变量未设置或为空时使用默认值。域名需提前解析至正确地址；程序不会申请域名、证书、配置 HTTPS 或建立隧道。

自定义代理端口时，宿主机与容器端口必须一致，例如同时使用 `-e SERVER_PORT=34443` 与 `-p 34443:34443/tcp`，并放行相应防火墙端口。建议使用非特权端口。

默认 REALITY 目标的可用性取决于 VPS 到该目标的网络；目标不通时可通过 `REALITY_SERVER_NAME` 更换。

## 通过已有 Cloudflare Tunnel 发布订阅

将已有 Tunnel 的订阅域名转发到 VPS 宿主机 `http://localhost:8080`，并在启动命令的镜像名之前增加：

```bash
-e SUB_BASE_URL='https://sub.example.com'
```

日志将显示：

```text
https://sub.example.com/<自动生成的令牌>/clash.yaml
```

将完整 URL 导入客户端即可。注意：

- `SUB_BASE_URL` 只改变日志中的地址，不会自动配置 Tunnel、DNS 或 HTTPS。
- 若基址包含路径前缀，反向代理需剥离该前缀再转发。
- 若 Tunnel 客户端在另一个容器中运行，该容器的 `localhost` 不是宿主机，需自行配置其访问宿主机的方式。
- 仅将订阅 HTTP 服务交给 Tunnel 转发；REALITY 代理端口不是订阅域名的 HTTP 流量入口。

## 数据保存与重建

身份和订阅令牌由容器管理并保存在 `/data`：

- **停止、启动或重启同一容器**：身份与令牌不变；环境变量不变时订阅 URL 不变。
- **不挂载数据卷，删除后新建容器**：生成新身份和令牌，需要重新导入订阅。
- **需要更新或重建后保留身份**：从首次部署起，在启动命令的镜像名之前增加命名卷：

  ```bash
  -v singbox-sub-data:/data
  ```

  重建时复用同一命名卷，无需手动维护其中的文件。不要让多个容器同时使用同一卷；请保护并备份数据卷，清空 `/data` 会丢失身份。

## 订阅与安全说明

- 订阅包含一个 REALITY 节点、默认选择该节点的 `PROXY` 组以及可选的 `DIRECT`。私网地址直连，其余流量走 `PROXY`；DNS 使用公共 DoH 服务，不包含地区分流规则，也不依赖额外规则集。
- 支持 xudp，服务端只需映射 TCP 代理端口。
- **将完整订阅 URL 视为凭据，不要公开。** 推荐通过 HTTPS 发布，并保持 8080 仅绑定宿主机回环地址。
- HTTP 只提供精确的 `/<token>/clash.yaml` 路径（GET/HEAD），不提供目录列表、私钥或内部状态文件。

## 故障排查

首先查看 `docker logs singbox-sub`：

- 参数无效、端口占用或服务启动失败时，容器会明确报错退出，不输出成功启动消息。
- 已有身份损坏或缺失时，程序拒绝自动替换。检查数据卷并恢复备份，不要通过清空数据卷保留原有身份。
- 代理或订阅服务意外退出时，容器失败退出，由 `--restart unless-stopped` 接管重启。
- 能下载订阅但不能代理时，检查 VPS 的公网地址、代理端口映射、防火墙及 REALITY 目标连通性；订阅可访问不代表代理端口可访问。

## 其他文档

- [开发与源码构建](docs/development.md)
- [镜像发布维护](docs/publishing.md)
- [验证记录与验收清单](docs/verification.md)
