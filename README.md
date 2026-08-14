# postkid

[简体中文](README.zh-CN.md) | English

A terminal-native API client built with Go and Bubble Tea — Postman for your terminal.

## Features

- Send `GET`, `POST`, `PUT`, `PATCH`, and `DELETE` requests
- Discover and invoke unary gRPC methods through Server Reflection, local `.proto` files, or a compiled protoset
- Edit query parameters, headers, request bodies, and authentication settings
- Use Basic Auth and Bearer Token authentication
- Organize requests into YAML collections
- Manage environments and interpolate `{{variables}}`
- Inspect status, latency, size, headers, and syntax-highlighted JSON responses
- Browse request history and reload previous requests
- Import browser "Copy as cURL" commands into a collection
- Import Postman Collection v2.1 JSON files
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
Saved gRPC requests also work with `run`; non-`OK` gRPC statuses exit non-zero.
gRPC requests are not recorded in history yet.

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

gRPC requests live alongside HTTP requests in the same collection:

```yaml
- name: check-health
  protocol: grpc
  url: "{{grpc_target}}"
  method: Check
  body: '{}'
  grpc:
    service: grpc.health.v1.Health
    method: Check
    metadata:
      authorization: "Bearer {{token}}"
    # Leave all descriptor fields out to use Server Reflection.
    # Or choose one local source:
    # proto_files: [proto/health.proto]
    # import_paths: [proto, third_party]
    # descriptor_set: descriptors/health.protoset
    tls:
      enabled: true
      server_name: api.example.com
```

gRPC supports plaintext/TLS, metadata, and JSON request/response bodies. When
`proto_files` is set, `import_paths` may list the directories used to resolve
imports. `descriptor_set` is an alternative to `proto_files`; the two sources
cannot be combined. If neither local source is configured, postkid uses Server
Reflection. Relative descriptor paths are resolved from the directory that
contains the Collection YAML file, not from the current working directory.
Streaming RPCs are not supported yet. Bodies continue to use `$EDITOR`.

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
| `e` | Edit the active tab; Body uses `$EDITOR` |
| `m` | Edit the request method and URL |
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
env use <name>   Switch to names reserved by CRUD actions
env new          Create an environment and edit its variables
env rename       Rename/edit an environment
env delete       Delete an environment with confirmation
collection new   Create a collection
collection rename Rename a collection
collection delete Delete a collection with confirmation
grpc new         Create and save a gRPC request
grpc edit        Edit the current gRPC request
grpc discover    Select a service/method from Reflection or local descriptors
grpc send        Invoke the current unary gRPC request
import curl      Paste, preview, and save a cURL command
import postman <path> Import a Postman Collection v2.1 JSON file
import postman-env <path> Import and select a Postman Environment JSON file
export curl      Copy the current request as a cURL command
history          Browse request history
new              Create a request
```

`import curl` opens a multiline paste form. Press `Ctrl+S` to parse, review the
request, choose a collection and request name, then press `Ctrl+S` again to
save. Importing never executes the command or reads `@file` arguments.

`import postman <path>` recursively imports requests, collection variables,
query parameters, headers, supported bodies, and Basic/Bearer authentication.
Folders are retained in flattened request names. Unsupported methods, auth/body
modes, and file form-data parts are rejected explicitly; an existing collection
is never overwritten.

`import postman-env <path>` imports enabled Postman Environment variables,
writes a new environment YAML under postkid's `environments/` directory, and
selects it automatically after a successful import. Disabled variables are
skipped. The imported values are stored as local YAML (including secrets), so
protect the data directory appropriately.

### Import a Postman Collection

Start postkid, press `:` to open the command palette, then enter:

```text
import postman /path/to/MyCollection.postman_collection.json
```

Quote a path that contains spaces:

```text
import postman "/path/to/My API.postman_collection.json"
```

This is a command-palette command inside the TUI, not a shell subcommand. The
imported collection is saved under postkid's `collections/` directory and is
selected after a successful import.

### Import a Postman Environment

Enter the following command in the TUI command palette:

```text
import postman-env /path/to/My Environment.postman_environment.json
```

Quote a path that contains spaces when needed:

```text
import postman-env "/path/to/My Environment.postman_environment.json"
```

Only enabled variables are imported. The new environment is saved under
postkid's `environments/` directory and selected after a successful import;
failed reads, parses, or saves keep the previous selection. Values are local
plain text in the YAML file, so do not import secrets into a directory you do
not trust.

## Development

Run the test suite:

```bash
go test ./...
```

For architecture notes and the project roadmap, see [design.md](design.md).

## License

No license has been specified yet.
