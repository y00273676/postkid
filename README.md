# postkid

[简体中文](README.zh-CN.md) | English

A terminal-native API client built with Go and Bubble Tea — Postman for your terminal.

## Features

- Send `GET`, `POST`, `PUT`, `PATCH`, and `DELETE` requests
- Edit query parameters, headers, request bodies, and authentication settings
- Use Basic Auth and Bearer Token authentication
- Organize requests into YAML collections
- Manage environments and interpolate `{{variables}}`
- Inspect status, latency, size, headers, and syntax-highlighted JSON responses
- Browse request history and reload previous requests
- Export requests as cURL commands
- Navigate entirely from the keyboard

## Installation

Requirements: Go 1.26 or later.

```bash
git clone https://github.com/y00273676/postkid.git
cd postkid
go build -o postkid ./cmd/postkid
```

To install `postkid` into your Go binary directory:

```bash
go install ./cmd/postkid
```

## Quick start

```bash
./postkid                 # use ~/.postkid as the data directory
./postkid -dir testdata   # launch with the included example data
./postkid run order/get-order               # run one saved request
./postkid run -env sandbox order            # run a collection
./postkid run ./collections/regression.yaml # run a collection file in CI
./postkid -version        # print the version
```

`run` is non-interactive. It prints the resolved URL, status, latency, size,
and response body, records history, and exits non-zero on transport errors or
HTTP 4xx/5xx responses. Use `-dir <path>` to select a data directory and
`-env <name>` to override `config.yaml`'s current environment.

On first launch, postkid creates the following structure under `~/.postkid`:

```text
~/.postkid/
├── config.yaml
├── collections/
├── environments/
└── history/
```

## Configuration

Create a collection in `~/.postkid/collections/order.yaml`:

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

Create an environment in `~/.postkid/environments/sandbox.yaml`:

```yaml
name: sandbox
variables:
  base_url: https://sandbox.example.com
  token: dev-token-xxx
```

Select the environment in `~/.postkid/config.yaml`:

```yaml
current_env: sandbox
```

When the same variable exists in multiple scopes, postkid uses this precedence:

```text
request > collection > environment
```

## Keyboard shortcuts

| Key | Action |
| --- | --- |
| `j` / `k` or `↑` / `↓` | Move up or down |
| `h` / `l` or `←` / `→` | Switch panels |
| `Enter` | Open the selected request |
| `n` | Create a request |
| `d` | Delete a request |
| `/` | Search requests |
| `Tab` / `Shift+Tab` | Switch between Params, Headers, Body, and Auth |
| `e` | Edit the body with `$EDITOR` |
| `s` / `Ctrl+R` | Send the request |
| `Ctrl+S` | Save the request to its collection |
| `:` | Open the command palette |
| `?` | Show help |
| `q` | Quit |

## Command palette

Press `:` and enter one of these commands:

```text
send             Send the current request
save             Save the current request
env <name>       Switch environments
export curl      Copy the current request as a cURL command
history          Browse request history
new              Create a request
```

> cURL import is not implemented yet.

## Development

Run the test suite:

```bash
go test ./...
```

For architecture notes and the project roadmap, see [design.md](design.md).

## License

No license has been specified yet.
