# TUI 版 Postman（postkid）

一个 Terminal-native 的 API 开发客户端。技术栈：Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea) + Bubbles + Lip Gloss。

---

## 1. TUI 概览

```
┌─────────────────────────────────────────────────────────────┐
│  TUI                                                        │
│                                                             │
│  Collections        Request                                 │
│  ├─ user-api        ┌─────────────────────────────────────┐ │
│  │  ├─ login        │ POST  https://api.xxx.com/login    │ │
│  │  ├─ profile      ├─────────────────────────────────────┤ │
│  │  └─ update       │ Params │ Headers │ Body │ Auth      │ │
│  └─ order-api       │                                     │ │
│     ├─ create       │ {                                   │ │
│     └─ detail       │   "username": "test"                │ │
│                    │ }                                   │ │
│                    └─────────────────────────────────────┘ │
│                                                             │
│                    Response                                 │
│                    200 OK  126ms  1.4KB                     │
│                    ┌─────────────────────────────────────┐ │
│                    │ {                                   │ │
│                    │   "code": 0,                        │ │
│                    │   "data": {...}                     │ │
│                    │ }                                   │ │
│                    └─────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. 核心架构

**核心建议：不要让 TUI 直接操作 `net/http`。** 在 TUI 与网络层之间引入一个 Application 层，把状态管理、请求执行、环境变量解耦。

```
                 ┌───────────────┐
                 │      TUI      │
                 │  Bubble Tea   │
                 └───────┬───────┘
                         │
                  Application
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
     RequestStore    HTTP Engine    Environment
          │              │              │
          ▼              ▼              ▼
       YAML/JSON      net/http       Variables
```

这样解耦后，同一套 Application 层可以同时支撑多种入口形态：

| 形态 | 用法 |
| --- | --- |
| TUI | `postkid` |
| CLI | `postkid run user-api/login` |
| CI | `postkid run ./collections/regression.yaml` |

---

## 3. V1 功能范围

**建议：千万别一开始照着 Postman 功能表抄。** V1 做下面这些就已经能用了。

| 功能 | V1 |
| --- | :---: |
| GET / POST / PUT / PATCH / DELETE | ✅ |
| URL 编辑 | ✅ |
| Query Params | ✅ |
| Headers | ✅ |
| JSON / Text Body | ✅ |
| Basic / Bearer Auth | ✅ |
| Response Body | ✅ |
| Status / Latency / Size | ✅ |
| JSON Pretty Print | ✅ |
| Collections | ✅ |
| 请求保存 | ✅ |
| Environment | ✅ |
| `{{variable}}` 变量 | ✅ |
| History | ✅ |
| curl 导出 | ✅ |
| Postman Collection v2.1 导入 | ✅ 第二阶段 |
| curl 导入 | ✅ V1.5 |
| gRPC unary | ✅ Reflection、本地 proto/protoset descriptor、metadata、TLS；streaming 后续 |
| WebSocket | 后面 |
| Pre / Post Script | 后面 |

其中 **curl 导入特别重要**——很多开发者的实际工作流是：

```
Chrome DevTools
    ↓
Copy as cURL
    ↓
TUI Postman
    ↓
修改参数
    ↓
Send
```

这会比手工创建请求舒服很多。V1.5 已加入非执行式 parser，处理 shell 引号转义、`\` 续行、`-H` / `-d` / `--data-raw` 等常见 flag；为保证导入安全，会拒绝 shell 操作符、命令替换、变量展开、`@file` 和其他不受支持的参数。

---

## 4. 数据存储

**第一版不需要 PostgreSQL、Redis、SQLite。** 直接用本地文件：

```
~/.postkid/
├── config.yaml
├── environments/
│   ├── dev.yaml
│   ├── sandbox.yaml
│   └── prod.yaml
├── collections/
│   ├── order.yaml
│   └── user.yaml
└── history/
    └── history.jsonl
```

### Collection 结构

```yaml
name: order

requests:
  - name: get-order
    method: GET
    url: "{{base_url}}/api/orders/{{order_id}}"
    headers:
      Authorization: "Bearer {{token}}"
    params:
      detail: "true"
```

### Environment 结构

```yaml
name: sandbox

variables:
  base_url: https://sandbox.example.com
  order_id: "123456"
  token: xxx
```

### 变量优先级

`{{variable}}` 有多个来源，**一开始就定死优先级**（高 → 低），避免后面改造成本：

1. **Request 级**：单个请求内定义的局部变量
2. **Collection 级**：Collection YAML 顶部声明的变量
3. **Environment 级**：当前选中 environment 的变量

同名变量按优先级覆盖。这个不一开始定清楚，后面调整成本很高。

### 核心卖点：Git 友好

API 请求以 YAML 文件存储，**可以直接用 Git 管理**。这甚至可能成为本工具相对 Postman 的核心卖点：

| | Postman | 本工具 |
| --- | --- | --- |
| Collection 存放位置 | 锁在 Postman 里 | 本地 YAML 文件 |
| 版本管理 | 不便 | `API Request = Code` → Git → Review / Diff / Branch |

---

## 5. TUI 交互设计

走 **Vim / k9s 风格**。

### 键位

| 键 | 动作 |
| --- | --- |
| `j` / `k` | 上下移动 |
| `h` / `l` | Panel 切换 |
| `Enter` | 打开 Request |
| `e` | Edit |
| `s` | Send |
| `n` | New Request |
| `d` | Delete |
| `/` | Search |
| `Tab` / `Shift+Tab` | Params → Headers → Body → Auth（正 / 反向） |
| `Ctrl+S` | Save |
| `Ctrl+R` | Send |
| `q` | Back |
| `?` | Help |

### Command Palette

最关键的设计：用 `:` 命令面板承载功能，避免功能增多后快捷键无限膨胀。

```
:send
:new
:save
:env sandbox
:env new
:env rename
:env delete
:collection new
:collection rename
:collection delete
:import curl
:import postman <path>
:export curl
:history
```

---

## 6. 定位

**有一个设计决定建议一开始就定下来：**

不要把它设计成「Postman 的 TUI 克隆」，而是设计成 **Terminal-native API development client**。

这样后面自然可以扩展：

```
HTTP
├── REST
├── GraphQL
├── SSE
└── WebSocket

RPC
├── gRPC
└── Connect

Collections
Environment
Secrets
History

Automation
├── assertions
├── variables
├── pre-request
├── post-request
└── collection runner
```

最终形态更像一个 **API Workspace**：

```
┌──────────────┐
│ API Workspace│
└───────┬──────┘
        │
   ┌────┼─────┐
   │    │     │
 HTTP  gRPC  WebSocket
   │
   ├── Collection
   ├── Environment
   ├── History
   ├── Scripts
   └── Tests
        │
   ┌────┴────┐
   │         │
  TUI       CLI
```

---

## 7. 难点判断

这个项目**技术难度不高，真正难的是 TUI 的交互设计**。HTTP 请求执行、Collection / Environment 持久化这些都比较成熟；下面这些才是后面最花时间的地方：

- **Body 编辑**（go/no-go 级难点，见下）
- 多 Panel 焦点管理
- 超长 Response 渲染
- JSON syntax highlighting
- 快捷键体系
- 异步请求时 UI 不阻塞

### Body 编辑：走外部 `$EDITOR`，别在 TUI 里造编辑器

Body 编辑是同类 TUI HTTP 工具最常见的翻车点。Bubbles 的 `textarea` 对多行 JSON 支持很弱：没有 auto-indent、没有括号匹配、没有语法高亮、嵌套结构里光标移动痛苦。试图在 TUI 里造一个代码编辑器会吃掉大量时间且体验很难做好。

**V1 决定**：Body 编辑直接调外部 `$EDITOR`（vim / nvim 等），TUI 内只做：

- 只读 JSON pretty-print 展示
- 常用字段的表单式编辑（key-value 表）

不要在 TUI 里实现完整的代码编辑器。

如果只做到一个真正能日常使用的 MVP，Go + Bubble Tea 完全够，而且项目规模可以控制得相当漂亮。
