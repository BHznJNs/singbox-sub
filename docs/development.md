# 开发与源码构建

本文面向开发者。部署与订阅使用见 [README](../README.md)，镜像自动发布见[发布维护](publishing.md)，历史测试结果见[验证记录](verification.md)。

## 源码构建

在项目根目录执行：

```bash
docker build -t singbox-sub .
```

按 [README](../README.md#快速开始) 中的命令启动，将 GHCR 镜像名替换为本地镜像名 `singbox-sub`。

主程序仅依赖 Go 标准库（Go 1.24+）。Dockerfile 使用 Go 多阶段构建，固定 sing-box `1.14.4`，运行时不会下载二进制；镜像构建阶段会执行单元测试与 `go vet`。

## 实现与数据管理

Go 主程序监督独立的 sing-box 进程，并运行订阅 HTTP 服务，无需 shell 启动脚本。只实现单用户、单节点、单份 Mihomo 订阅，不引入 Compose、数据库、用户管理或 Tunnel 管理。

- `/data/identity/state.json` 保存 UUID、REALITY 密钥对、Short ID 和 256 位安全随机订阅令牌。
- 私钥不写入订阅，不主动输出到日志；身份和生成文件使用仅所有者可读写的权限。
- `/data/sing-box.json` 与 `/data/clash.yaml` 每次启动从同一份身份生成；启动前执行 `sing-box check`。
- HTTP 处理器只匹配令牌对应的订阅路径，不暴露 `/data` 静态目录。
- 只有代理监听与订阅 HTTP 就绪检查均通过后，才输出成功信息和订阅 URL。
- 正常停止时关闭 HTTP 并停止 sing-box；任一内部服务异常退出，主程序清理服务并失败退出，使 Docker 重启策略生效。

### 身份损坏与初始化边界

已有身份缺失、不完整、JSON 损坏、字段无效或密钥对不匹配时，必须明确报错，不能静默覆盖身份。身份目录同时充当首次初始化标记：

- 初始化被中断时保留标记，后续启动拒绝静默重建。
- 整个身份目录丢失，但 `/data` 中仍有生成文件时，同样拒绝重建。
- 整个 `/data` 被清空时，无法区分它与全新目录。
- REALITY 目标的临时网络故障不应导致重新生成身份。

## 测试

在项目根目录执行：

```bash
go test ./...
go vet ./...
```

可选真实内核集成测试（提供已下载的 sing-box 和 Mihomo 可执行文件绝对路径，以下为 Bash）：

```bash
SING_BOX_TEST_BINARY=/path/to/sing-box MIHOMO_TEST_BINARY=/path/to/mihomo go test -v ./...
```

未提供可执行文件时，集成测试明确跳过。测试会监听本机端口，运行前确保 8080 空闲。

集成测试先检查未修改的订阅，再为本机回环测试调整客户端监听端口，并将测试副本的路由改为全部走代理，避免生产订阅的私网直连规则绕过待测链路。下载用于测试的内核放在 `.tools/`，该目录被 Git 和 Docker 构建上下文忽略，不属于运行时依赖。

## 交叉编译

例如在 Bash 中构建 Linux amd64：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .
```

将 `GOARCH` 替换为 `arm64` 可构建 ARM64 主程序。Docker BuildKit 也支持按目标架构构建镜像。
