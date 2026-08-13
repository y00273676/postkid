# postkid

简体中文 | [English](README.md)

一款使用 Go 和 Bubble Tea 构建的终端原生 API 客户端——终端里的 Postman。

## 功能特性

- 发送 `GET`、`POST`、`PUT`、`PATCH` 和 `DELETE` 请求
- 编辑查询参数、请求头、请求体和认证配置
- 支持 Basic Auth 和 Bearer Token 认证
- 使用 YAML Collection 组织请求
- 管理环境并插值替换 `{{变量}}`
- 查看状态码、耗时、响应大小、响应头和带语法高亮的 JSON 响应
- 浏览请求历史并重新载入历史请求
- 将请求导出为 cURL 命令
- 全键盘操作

## 安装

环境要求：Go 1.26 或更高版本。

```bash
git clone https://github.com/y00273676/postkid.git
cd postkid
go build -o postkid ./cmd/postkid
```

也可以将 `postkid` 安装到 Go 的二进制目录：

```bash
go install ./cmd/postkid
```

## 快速开始

```bash
./postkid                 # 使用 ~/.postkid 作为数据目录
./postkid -dir testdata   # 使用项目自带的示例数据启动
./postkid run order/get-order               # 执行一个已保存请求
./postkid run -env sandbox order            # 执行整个集合
./postkid run ./collections/regression.yaml # 在 CI 中执行集合文件
./postkid -version        # 输出版本号
```

`run` 是非交互模式：它会输出解析后的 URL、状态、耗时、大小和响应正文，
同时记录 History；网络错误或 HTTP 4xx/5xx 响应会返回非零退出码。可用
`-dir <路径>` 指定数据目录，用 `-env <名称>` 覆盖 `config.yaml` 中的当前环境。

首次启动时，postkid 会在 `~/.postkid` 下创建以下目录结构：

```text
~/.postkid/
├── config.yaml
├── collections/
├── environments/
└── history/
```

## 配置

在 `~/.postkid/collections/order.yaml` 中创建 Collection：

```yaml
name: order
variables:
  order_id: "123456"
requests:
  - name: get-order
    method: GET
    url: "{{base_url}}/api/orders/{{order_id}}"
    headers:
      Accept: application/json
    params:
      detail: "true"
    auth_type: bearer
    auth_token: "{{token}}"
```

在 `~/.postkid/environments/sandbox.yaml` 中创建 Environment：

```yaml
name: sandbox
variables:
  base_url: https://sandbox.example.com
  token: dev-token-xxx
```

在 `~/.postkid/config.yaml` 中选择当前环境：

```yaml
current_env: sandbox
```

同名变量存在于多个作用域时，postkid 按以下优先级取值：

```text
request > collection > environment
```

## 快捷键

| 按键 | 操作 |
| --- | --- |
| `j` / `k` 或 `↑` / `↓` | 上下移动 |
| `h` / `l` 或 `←` / `→` | 切换面板 |
| `Enter` | 打开选中的请求 |
| `n` | 新建请求 |
| `d` | 删除请求 |
| `/` | 搜索请求 |
| `Tab` / `Shift+Tab` | 切换 Params、Headers、Body 和 Auth |
| `e` | 使用 `$EDITOR` 编辑请求体 |
| `s` / `Ctrl+R` | 发送请求 |
| `Ctrl+S` | 将请求保存到 Collection |
| `:` | 打开命令面板 |
| `?` | 显示帮助 |
| `q` | 退出 |

## 命令面板

按下 `:` 后输入以下命令：

```text
send             发送当前请求
save             保存当前请求
env <name>       切换环境
export curl      将当前请求复制为 cURL 命令
history          浏览请求历史
new              新建请求
```

> cURL 导入功能尚未实现。

## 开发

运行测试：

```bash
go test ./...
```

架构说明和项目规划请参阅 [design.md](design.md)。

## 许可证

项目暂未指定许可证。
