---
doc_type: requirement
slug: platform-release-artifacts
pitch: 让发布人员按目标平台拿到互不混淆的 Codis 构建产物
status: current
last_reviewed: 2026-06-01
implemented_by: [system-overview]
tags: [build, release, operations]
---

# 按目标平台拿到互不混淆的构建产物

## 用户故事

- 作为发布人员，我希望一次构建能产出明确按平台区分的 Codis 包，而不是手动猜当前 `bin/` 里的二进制适合哪台机器。
- 作为本地开发者，我希望默认 `make` 仍然快速构建当前机器可运行的产物，而不是因为发布矩阵缺少跨平台工具链就卡住日常开发。
- 作为运维人员，我希望 Linux 和 macOS 的发布物互相隔离，而不是把不同平台的 Redis helper、FE assets 和配置副本混在同一个目录里。
- 作为维护者，我希望跨平台构建条件不满足时明确失败，而不是生成一个目录名像 Linux、实际内容却来自 host 的产物。

## 为什么需要

Codis 的发布物不是单个 Go 二进制，还包含 Codis Server、Redis helper、FE assets 和默认配置。只把所有文件放进根 `bin/` 时，发布人员很难稳定区分目标平台，也容易把 host 产物误当成 Linux 产物带到部署环境。

## 怎么解决

默认构建继续服务本地开发，生成当前机器可运行的根 `bin/` 产物；发布构建通过显式入口按平台矩阵生成独立目录，每个目录就是一个目标平台的完整产物集合。构建开始前会检查目标平台和必要工具链，条件不满足时直接报错，避免错误平台产物进入发布目录。

## 边界

- 它不保证在任意 host 上完整交叉构建任意 target；完整跨平台产物仍依赖目标平台构建环境或明确配置好的 C/cgo cross toolchain。
- 它不改变 Codis 运行期协议、proxy 路由、dashboard 行为或 Redis 数据格式。
- 它不负责 Docker 镜像、CI 发布流水线、上传 release 包或生产部署编排。
- 它不是纯 Go cross build 能力；当前 proxy 和 Redis Server 的完整发布物仍包含 C/cgo 构建边界。
