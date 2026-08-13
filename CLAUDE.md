# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

postkid — Terminal-native API 客户端（TUI 版 Postman）。技术栈：Go + Bubble Tea + Bubbles + Lip Gloss。

## 构建与测试

```bash
go build -o postkid ./cmd/postkid
go install ./cmd/postkid        # 安装到 $GOPATH/bin
go test ./...                  # 运行全部测试
go test -run TestXxx ./...     # 运行单个测试（如 TestResolveRequest）
```

运行（使用项目内置示例数据，指向 sandbox.example.com）：

```bash
go run ./cmd/postkid -dir testdata
```

## 架构

```
TUI (bubbletea)
  │
  ▼
App (application 层门面，不 import bubbletea，可被 CLI/CI 复用)
  │
  ├── Store      → YAML 持久化（collections/*.yaml, environments/*.yaml）
  ├── HTTP Engine → net/http 执行请求
  └── Environment → 变量合并与 {{variable}} 替换
```

### 分层说明

- **`cmd/postkid/main.go`** — 入口，解析 flag，创建 App，启动 Bubble Tea 程序
- **`internal/model/`** — 核心数据结构：Request, Collection, Environment, Response, ResolvedRequest
- **`internal/app/`** — Application 层门面：加载数据、变量替换、请求发送、保存回写
- **`internal/tui/`** — Bubble Tea TUI 实现：三面板布局（List | Request | Response），命令面板，Tab 切换
- **`internal/httpengine/`** — HTTP 客户端封装，30s 超时，JSON pretty-print，10MB body 上限
- **`internal/env/`** — `{{variable}}` 合并与替换，优先级：request > collection > environment
- **`internal/store/`** — YAML 加载/原子写回（临时文件 + rename）
- **`internal/config/`** — `~/.postkid/` 数据目录初始化与 config.yaml 管理
- **`internal/editor/`** — 用 `$EDITOR` 暂停 TUI 编辑请求 body

### 数据目录结构

```
~/.postkid/
├── config.yaml              # current_env 等配置
├── collections/*.yaml       # 请求集合
├── environments/*.yaml      # 环境变量
└── history/                 # 预留
```

### 变量优先级

1. Request 级变量（`request.variables`）
2. Collection 级变量（`collection.variables`）
3. Environment 级变量（`environment.variables`）
同名变量按优先级覆盖。

## 已实现功能（V1）

| 功能 | 状态 |
|---|---|
| GET/POST/PUT/PATCH/DELETE | ✅ model 支持，测试覆盖 GET/POST |
| URL 编辑 | ✅ `m` 打开 Method / URL 表单 |
| Query Params | ✅ Params tab 展示和 key-value 编辑 |
| Headers | ✅ Headers tab 展示和 key-value 编辑 |
| JSON/Text Body | ✅ Body tab 展示 + $EDITOR 编辑 |
| Auth (Basic/Bearer) | ✅ Auth tab 表单，发送时自动生成 Authorization header |
| Response Body | ✅ JSON pretty-print |
| Status/Latency/Size | ✅ 状态栏展示 |
| Collections | ✅ 加载、选择、保存及 `:collection` CRUD |
| Environment | ✅ 切换、变量编辑、`:env` CRUD、持久化 current_env |
| `{{variable}}` 变量 | ✅ 三层优先级合并 |
| History | ✅ `:history` 浏览，发送后自动记录到 jsonl |
| curl 导出 | ✅ `:export curl` 复制到剪贴板 |
| curl 导入 | ✅ `:import curl` 安全解析、预览并保存 |
| New Request | ✅ `:new` / `n` 表单选择 Collection 并填写请求信息 |
| Delete Request | ✅ `d` 键并确认 |
| Search | ✅ `/` 键实时过滤列表 |

## 关键设计决策

- Body 编辑走外部 `$EDITOR`，TUI 内不做完整编辑器
- 存储用本地 YAML 文件（Git 友好），不用数据库
- App 层不 import bubbletea，未来可被 CLI/CI 复用
- 原子写用临时文件 + rename 防止崩溃损坏
- 请求发送是同步的，TUI 中用 `tea.Sequence` 保证 sending 状态先于异步响应
- Auth 字段 → Authorization header 在 ResolveRequest 中处理，不覆盖显式 Header
- History 用 JSONL 追加写，最多保留 500 条
