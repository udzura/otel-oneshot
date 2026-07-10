# otel-oneshot

A non-interactive CLI that takes a single OTLP/JSON trace file and renders the
span timeline as ASCII art to standard output **exactly once**.

It has no key-input loop or screen refresh. Display settings are fixed at startup
from flags/config file, and it runs as a one-directional pipeline:
`parse → filter → layout → render → stdout → exit`. It works fine with pipes,
redirects, and in CI environments.

```
Trace: 4bf92f3577b34da6a3ce929d0e0e4736  (root: HTTP GET /api/orders, duration: 842ms)

span                               0ms                        421ms                      842ms
└─ HTTP GET /api/orders            ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓ 842ms
   └─ gateway.route                 ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓  801ms
      ├─ auth-service.verify           ▓▓▓▓                                                     52ms
      └─ db-query [ERROR]                        ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓   612ms
         └─ db.connection.acquire                ▓▓                                             30ms
```

## Install / Build

```sh
go build -o otel-oneshot .
# or run directly
go run . [input.json] [flags]
```

Requires Go 1.25 or newer.

## Usage

```
otel-oneshot [input.json] [flags]
```

- If `input.json` is omitted (or `-` is given), input is read from **stdin**.
- Flags and the input path may be given in **any order**.

```sh
otel-oneshot trace.json --width 100
otel-oneshot --width 100 trace.json          # same as above
cat trace.json | otel-oneshot --color never  # from stdin
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--config` | string | `""` | Path to a YAML config file |
| `--trace-id` | string | `""` | Target trace ID. Empty uses the first trace found |
| `--root-span-id` | string | `""` | Render only the subtree rooted at this span ID |
| `--root-span-name` | string | `""` | Root at the first span matching this name (id wins when both given) |
| `--max-depth` | int | `0` (unlimited) | Max tree depth to display. Pruned children show as `... (N children hidden)` |
| `--top-n` | int | `0` (unlimited) | Show only the N longest spans by duration (ancestors are kept to preserve structure) |
| `--width` | int | `0` (auto) | Output width. 0 detects the terminal width, falling back to 120 |
| `--show-attributes` | string (CSV) | `""` | Attribute keys to annotate after the span name (e.g. `http.status_code,db.statement`) |
| `--highlight-errors` | bool | `true` | Highlight ERROR spans. Disable with `--highlight-errors=false` |
| `--color` | string | `auto` | `auto` \| `always` \| `never` (auto detects a TTY and honors `NO_COLOR`) |
| `--sort` | string | `start_time` | `start_time` \| `duration` (ordering of siblings) |
| `--time-unit` | string | `auto` | `auto` \| `s` \| `ms` \| `us` \| `ns` |

### Precedence

**CLI flag > YAML config file > default value**

Only flags explicitly set on the command line override the config file.

## Config file (YAML)

```yaml
trace_id: ""
root_span_name: "HTTP GET /api/orders"
max_depth: 10
top_n: 0
width: 0
show_attributes: ["http.status_code", "db.statement"]
highlight_errors: true
color: auto
sort: start_time
time_unit: auto
```

```sh
otel-oneshot --config config.yaml trace.json
```

## Examples

```sh
# Show only a subtree, with a depth limit
otel-oneshot trace.json --root-span-name "HTTP GET /api/orders" --max-depth 3

# Focus on the 10 slowest spans (ancestors are retained)
otel-oneshot trace.json --top-n 10 --sort duration

# Annotate attributes
otel-oneshot trace.json --show-attributes http.status_code,db.statement

# Nanosecond precision (for sub-microsecond traces)
otel-oneshot trace.json --time-unit ns

# Select a specific trace from a batch
otel-oneshot batch.json --trace-id 4bf92f3577b34da6a3ce929d0e0e4736
```

## Exit codes

| Situation | Behavior |
|---|---|
| Input file missing / unreadable | Error to stderr, exit 1 |
| Whole JSON is invalid | Error to stderr, exit 1 |
| Individual span missing fields | Warning to stderr, skip that span and continue |
| `--trace-id` not present | Error to stderr, exit 1 |
| `--root-span-id` / `--root-span-name` not found | Error to stderr, exit 1 |
| Multiple traces without `--trace-id` | Warning to stderr (states which was chosen), continue |
| Zero spans after filtering | Print `no spans to display` to stdout, exit 0 |

## Design

See [DESIGN.md](./DESIGN.md) for the internal structure, data model, and the
responsibilities of each layer. Packages follow a one-directional dependency
chain: `otlp → domain → filter → layout → render`.

## Development

```sh
go test ./...                                   # run all tests
go test ./... -run TestGoldenSample -update     # regenerate golden files
```
