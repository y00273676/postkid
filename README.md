# tpost

Terminal-native API 客户端（TUI 版 Postman）。设计与决策见 [design.md](design.md)。

## 安装

```bash
go build -o tpost ./cmd/tpost
# 或安装到 PATH
go install ./cmd/tpost
```

## 用法

```bash
tpost                 # 用 ~/.tpost 数据目录
tpost -dir testdata   # 用项目自带示例数据（指向 sandbox.example.com，仅演示）
tpost -version
```

首次运行会在 `~/.tpost/` 下自动创建 `collections/`、`environments/`、`history/` 目录。

## 数据目录

```
~/.tpost/
├── config.yaml              # current_env 等配置
├── collections/*.yaml       # 请求集合
└── environments/*.yaml      # 环境变量
```

Collection 示例：

```yaml
name: order
variables:
  order_id: "123456"
requests:
  - name: get-order
    method: GET
    url: "{{base_url}}/api/orders/{{order_id}}"
    headers:
      Authorization: "Bearer {{token}}"
    params:
      detail: "true"
```

Environment 示例：

```yaml
name: sandbox
variables:
  base_url: https://sandbox.example.com
  token: dev-token-xxx
```

变量优先级：`request > collection > environment`，同名前者覆盖后者。

## 键位

| 键 | 动作 |
| --- | --- |
| `j` / `k` | 上下移动 |
| `h` / `l` | Panel 切换（List ↔ Request ↔ Response） |
| `Enter` | 打开选中请求 |
| `Tab` / `Shift+Tab` | 切 Params → Headers → Body |
| `e` | 用 `$EDITOR` 编辑 body |
| `s` / `Ctrl+R` | 发送 |
| `Ctrl+S` | 保存（回写 collection YAML） |
| `:` | 命令面板 |
| `?` | 帮助 |
| `q` | 退出 |

## 命令面板

```
:send            发送当前请求
:save            保存
:env <name>      切换环境
```

`new` / `export curl` / `import curl` / `history` 暂未实现。

## 测试

```bash
go test ./...
```
