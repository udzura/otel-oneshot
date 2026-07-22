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

### Homebrew

```sh
brew install --cask udzura/tap/otel-oneshot
```

### From source

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
| `--fold` | string (repeatable) | `[]` | Collapse the subtree of any span matching the pattern. The span stays; its descendants are replaced by a `... (N children hidden by --fold)` marker |
| `--hide` | string (repeatable) | `[]` | Drop any span matching the pattern and reparent its children onto the nearest surviving ancestor (elides intermediate noise) |
| `--only` | string (repeatable) | `[]` | Inverse of `--hide`: drop any span matching **none** of the patterns, reparenting its children. Only spans matching at least one `--only` pattern remain |
| `--match-mode` | string | `regex` | How `--fold`/`--hide`/`--only` patterns match: `regex` \| `exact` |
| `--highlight-errors` | bool | `true` | Highlight ERROR spans. Disable with `--highlight-errors=false` |
| `--color` | string | `auto` | `auto` \| `always` \| `never` (auto detects a TTY and honors `NO_COLOR`) |
| `--sort` | string | `start_time` | `start_time` \| `duration` (ordering of siblings) |
| `--time-unit` | string | `auto` | `auto` \| `s` \| `ms` \| `us` \| `ns` |
| `--version` | bool | | Print the version (`otel-oneshot vX.Y.Z`) and exit |

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
fold_patterns: ["^App#render"]
hide_patterns: ["^Sinatra::", "^Rack::"]
only_patterns: []
match_mode: regex
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

# Elide framework noise: drop Sinatra/Rack spans, keeping the app spans nested
# under them (children are reparented). --hide/--fold/--only are repeatable.
otel-oneshot trace.json --hide '^Sinatra::' --hide '^Rack::'

# Keep only db.* spans, reparenting everything else out of the way
otel-oneshot trace.json --only '^db\.'

# Collapse a subtree you don't want to expand (keeps the node, hides descendants)
otel-oneshot trace.json --fold '^App#render_template'

# Match literal span names instead of regex
otel-oneshot trace.json --hide 'Hash#[]' --match-mode exact

# Annotate attributes
otel-oneshot trace.json --show-attributes http.status_code,db.statement

# Nanosecond precision (for sub-microsecond traces)
otel-oneshot trace.json --time-unit ns

# Select a specific trace from a batch
otel-oneshot batch.json --trace-id 4bf92f3577b34da6a3ce929d0e0e4736
```

## Folding and hiding spans

`--hide`, `--fold`, and `--only` all prune the tree, but differently. They are
especially useful for "trace everything" dumps (e.g. from a framework request)
where most spans are framework/gem noise. All three flags are **repeatable**
and interpret their pattern per `--match-mode` (`regex`, the default, or
`exact`).

Given this trace:

```
└─ GET /orders/2
   └─ tid=16
      └─ Framework.dispatch
         └─ Framework.middleware
            └─ Framework.route
               └─ App#handle
                  └─ App#render
                     ├─ App#partial
                     └─ App#serialize
                        └─ JSON.generate
```

### `--hide` — drop matching spans, reparent their children

Removes each matching span and lifts its children onto the nearest surviving
ancestor. Use it to strip intermediate framework layers while keeping the app
spans that were nested inside them.

```sh
# Drop the Framework.* layers
otel-oneshot sample.json --hide '^Framework\.'

# Repeat for several patterns
otel-oneshot sample.json --hide '^Framework\.' --hide '^JSON\.'
```

`--hide '^Framework\.'` yields (the three `Framework.*` spans are gone and
`App#handle` moves up under `tid`):

```
└─ GET /orders/2
   └─ tid=16
      └─ App#handle
         └─ App#render
            ├─ App#partial
            └─ App#serialize
               └─ JSON.generate
```

### `--only` — the inverse of `--hide`: keep only matching spans

Drops each span matching **none** of the `--only` patterns and lifts its
children onto the nearest surviving ancestor, same reparenting as `--hide`.
Use it when you only care about a handful of span names buried in a big trace.

```sh
# Keep only App#* spans; everything else (Framework.*, JSON.generate) is
# dropped and its children reparented.
otel-oneshot sample.json --only '^App#'
```

`--only '^App#'` yields (only the two `App#*` spans remain, promoted to root):

```
└─ App#handle
   └─ App#render
      ├─ App#partial
      └─ App#serialize
```

`--only` and `--hide` can be combined: a span survives only if it matches an
`--only` pattern **and** matches no `--hide` pattern.

### `--fold` — keep the span, collapse its subtree

Keeps each matching span but replaces its descendants with a
`... (N children hidden by --fold)` marker. Use it to acknowledge a subtree
exists without expanding its internals.

```sh
otel-oneshot sample.json --fold '^App#render$'
```

yields:

```
               └─ App#handle
                  └─ App#render
                     └─ ... (2 children hidden by --fold)
```

### Combined, and exact matching

```sh
# Strip the framework AND fold the render subtree
otel-oneshot sample.json --hide '^Framework\.' --fold '^App#render$'

# Match a literal name containing regex metacharacters
otel-oneshot sample.json --hide 'Hash#[]' --match-mode exact
```

> If a marker is truncated (e.g. `... (2 ch...`) on a deep tree, widen the
> output with `--width 120`: the tree pane is capped at 40% of the total width.

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
