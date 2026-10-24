# 镜像发布维护

本文面向仓库维护者。镜像部署与订阅使用见 [README](../README.md)。

## 工作流

[`.github/workflows/publish-image.yml`](../.github/workflows/publish-image.yml) 构建并推送 `linux/amd64` 和 `linux/arm64` 多架构镜像，复用当前 Dockerfile 中的测试与静态检查，并启用 GitHub Actions 构建缓存。

| 触发方式 | 发布标签 |
| --- | --- |
| 推送到 `main` | `latest`、`sha-<完整提交 SHA>` |
| 推送 `v*` 标签，例如 `v1.0.0` | `v1.0.0`、`sha-<完整提交 SHA>`，不更新 `latest` |
| Actions 页面手动运行 | 始终发布 SHA 标签；选择 `main` 时同时发布 `latest`，选择版本标签时同时发布对应标签 |

镜像名称自动取小写仓库名，当前为 `ghcr.io/bhznjns/singbox-sub`。工作流只在推送或手动触发时发布，不对 Pull Request 自动发布镜像；添加工作流文件并不代表镜像已经发布。

## 认证与权限

工作流使用 GitHub 自动提供的 `GITHUB_TOKEN`，仅授予 `contents: read` 和 `packages: write`，无需额外配置 PAT 或仓库 Secret。所用 Actions 固定到提交 SHA。

首次发布时：

1. 将工作流提交并推送到 GitHub 的 `main`，或在 Actions 页面选择 **Publish image to GHCR → Run workflow**。
2. 确认仓库允许 GitHub Actions 运行、允许使用工作流引用的 Actions，且组织策略没有禁止 `packages: write`。
3. 若同名 GHCR 包已存在，在包的 **Package settings → Manage Actions access** 中授予本仓库写入权限。
4. 发布成功后，在个人或组织的 **Packages → singbox-sub → Package settings → Change visibility** 中按需设为 **Public**，否则匿名用户不能直接拉取。仓库公开不代表新建的 GHCR 包自动公开。

## 版本与使用文档

`latest` 随 `main` 更新，版本标签发布不会更新 `latest`。需要固定部署版本时，使用已发布的版本标签或 SHA 标签，运行示例见 [README](../README.md#快速开始)。

仓库迁移或重命名后，工作流会自动采用新的镜像名称，但 README 中的具体镜像示例需要同步修改。是否已经发布、包是否公开及云端构建是否成功，应以实际 Actions 和 Packages 状态为准，不以本地文档或语法检查作为发布成功依据。
