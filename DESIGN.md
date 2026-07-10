# otel-oneshot Design Doc

## 1. 概要

`otel-oneshot` は、OTLP (OpenTelemetry Protocol) 形式のJSONファイルを1つ受け取り、
そのトレース(またはサブツリー)のspan timelineをASCIIアートとしてターミナルに
1回だけ出力するCLIツールである。

**重要な前提: このツールにインタラクションは一切ない。**
起動時に読み込まれた設定(CLIフラグ / 設定ファイル)に基づき、パース→フィルタ→
レイアウト計算→描画→stdout出力→プロセス終了、という一方向パイプラインとして動作する。
キー入力待ち、画面更新ループ、状態遷移は実装しない。

### 1.1 ゴール

- OTLP JSON(1ファイル)を入力として受け取り、span階層とタイムラインバーを
  ASCIIアートで標準出力に描画する。
- 表示対象(root span、深さ、時間範囲など)はすべて起動時オプションで確定できる。
- パイプ・リダイレクト・CI環境で問題なく動作する(TTY依存の機能は自動フォールバック)。
- 出力は再現可能(同じ入力+同じオプション→同じ出力)。

### 1.2 非ゴール

- キーボード操作によるズーム/スクロール/フィルタ変更は実装しない。
- 複数ファイルのマージ、リアルタイムのOTLP受信(gRPC/HTTPコレクタとしての機能)は
  スコープ外。あくまで「1つのJSONファイル → 1回の出力」。
- Web UIやHTML出力は将来検討事項とし、v1では対象外。

---

## 2. 入力仕様

### 2.1 OTLP JSON

標準的なOTLP/JSON (`resourceSpans[].scopeSpans[].spans[]`) 構造を想定する。

```json
{
  "resourceSpans": [{
    "resource": { "attributes": [ { "key": "service.name", "value": {"stringValue": "..."} } ] },
    "scopeSpans": [{
      "spans": [{
        "traceId": "hex string",
        "spanId": "hex string",
        "parentSpanId": "hex string or empty",
        "name": "string",
        "startTimeUnixNano": "string (uint64 as string)",
        "endTimeUnixNano": "string (uint64 as string)",
        "attributes": [ {"key": "...", "value": {...}} ],
        "events": [ {"timeUnixNano": "string", "name": "string", "attributes": [...]} ],
        "status": { "code": "STATUS_CODE_OK|STATUS_CODE_ERROR|STATUS_CODE_UNSET" }
      }]
    }]
  }]
}
```

**パース時の注意点(実装必須)**:

1. `startTimeUnixNano` / `endTimeUnixNano` / `timeUnixNano` は **JSON文字列**として
   来る場合と数値として来る場合の両方があり得る。`uint64` として安全にパースできる
   カスタムUnmarshalerを用意すること(floatを経由すると精度が落ちるため禁止)。
2. 1ファイルに **複数の traceId が混在する場合がある**(バッチエクスポート)。
   `--trace-id` 未指定時は「最初に出現したtraceId」をデフォルト対象とし、
   複数存在する場合は stderr に警告を出す(インタラクティブな選択はしない)。
3. `parentSpanId` が空文字列、または該当spanが存在しない場合はroot span扱いにする。
4. 不正なJSON、必須フィールド欠損spanは **スキップしてstderrに警告**、
   処理は継続する(1つの壊れたspanで全体を失敗させない)。

### 2.2 起動オプション

CLIフラグと設定ファイル(YAML)の両方をサポートし、優先順位は
**CLIフラグ > 設定ファイル > デフォルト値**とする。

```
otel-oneshot [input.json] [flags]
```

| フラグ | 型 | デフォルト | 説明 |
|---|---|---|---|
| `--config` | string | "" | 設定ファイル(YAML)のパス |
| `--trace-id` | string | "" | 対象trace ID。空なら最初に出現したtraceIdを使用 |
| `--root-span-id` | string | "" | このspan IDをrootとしてサブツリー表示。空ならtrace全体 |
| `--root-span-name` | string | "" | この名前と一致する最初のspanをrootにする(`--root-span-id`と併用時はidを優先) |
| `--max-depth` | int | 0 (無制限) | 表示するツリーの深さ上限 |
| `--top-n` | int | 0 (無制限) | duration降順で上位N件のspanのみ表示(親子関係は保持) |
| `--width` | int | 0 (auto) | 出力全体の文字幅。0なら端末幅を検出、検出不可なら120 |
| `--show-attributes` | []string | [] | タイムライン行に併記するattributeキーのカンマ区切りリスト |
| `--highlight-errors` | bool | true | status=ERRORのspanを強調表示するか |
| `--color` | string | "auto" | `auto` \| `always` \| `never` |
| `--sort` | string | "start_time" | `start_time` \| `duration` |
| `--time-unit` | string | "auto" | duration表記の単位。`auto`\|`ms`\|`us`\|`ns` |

設定ファイル例 (`config.yaml`):

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

---

## 3. アーキテクチャ

```
main.go
 │
 ├─ config           … フラグ/YAMLのパースとマージ、バリデーション
 │
 ├─ otlp             … OTLP JSON → 内部Spanモデルへの変換
 │    parse.go        raw JSON構造体定義 + Unmarshal
 │    convert.go      raw → domain.Span への変換
 │
 ├─ domain           … 内部データモデル(パッケージ外部からの依存を持たない)
 │    span.go         Span構造体、Tree構築
 │
 ├─ filter           … root抽出、max-depth適用、top-n適用、sort適用
 │    filter.go
 │
 ├─ layout           … 時間軸→列位置への変換、行(Row)リストの生成
 │    layout.go
 │
 ├─ render           … Rowリスト → ASCII文字列
 │    tree_render.go  左側のツリー表現
 │    bar_render.go   右側のタイムラインバー
 │    color.go        TTY判定、ANSIカラー適用
 │
 └─ main.go           … 上記を順に呼び出すだけ。ロジックを持たない
```

**設計原則**: 各パッケージは一方向にのみ依存する
(`otlp → domain → filter → layout → render`)。
`render` 以外のパッケージは端末幅やANSIカラーのようなUI都合を一切知らない。
これにより、将来HTML出力などを追加する際は `render` パッケージだけ差し替えればよい。

---

## 4. 内部データモデル (`domain` パッケージ)

```go
package domain

type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOK
	StatusError
)

type Event struct {
	TimeNanos  uint64
	Name       string
	Attributes map[string]string
}

type Span struct {
	SpanID       string
	ParentSpanID string
	TraceID      string
	Name         string
	ServiceName  string
	StartNanos   uint64
	EndNanos     uint64
	Attributes   map[string]string
	Events       []Event
	Status       StatusCode

	Children []*Span // ツリー構築後に設定される
	Depth    int      // ツリー構築後に設定される
}

func (s *Span) DurationNanos() uint64 {
	if s.EndNanos <= s.StartNanos {
		return 0
	}
	return s.EndNanos - s.StartNanos
}

// Trace はパース後の1トレース分のデータをまとめたもの
type Trace struct {
	TraceID   string
	Roots     []*Span // parentが存在しない/見つからないspanの集合
	AllSpans  map[string]*Span // spanId -> Span
	MinStart  uint64
	MaxEnd    uint64
}
```

**ツリー構築アルゴリズム** (`domain.BuildTree`):

1. 全spanを `spanId -> *Span` map に格納。
2. 各spanについて `parentSpanId` を引き、存在すれば `parent.Children` に追加。
   存在しない/空なら `Trace.Roots` に追加。
3. BFSまたはDFSで各spanに `Depth` を設定(rootは0)。
4. `Children` は `StartNanos` 昇順にソートしておく(表示順の安定性のため)。
5. 全spanを走査し `MinStart = min(start)` / `MaxEnd = max(end)` を計算。

---

## 5. フィルタ層 (`filter` パッケージ)

適用順序は固定とする(この順でないと `max-depth` や `top-n` の意味が変わるため):

1. **root解決**: `--root-span-id` / `--root-span-name` が指定されていれば、
   該当spanを新たな仮想rootとし、そのsubtreeだけを対象にする。
   両方未指定なら `Trace.Roots` 全体を対象とする。
2. **max-depth適用**: root解決後のツリーに対し、指定深さを超える子孫を除去する
   (除去した場合は該当ノードに「...(N children hidden)」の情報を持たせておき、
   renderで表示できるようにする)。
3. **top-n適用**: 残ったspan群を `DurationNanos()` 降順でソートし、上位N件の
   spanIDセットを求める。このセットに含まれないspanは非表示にするが、
   **祖先spanは表示上の構造維持のため強制的に残す**(そうしないとツリーが
   バラバラになるため)。
4. **sort適用**: 兄弟span間の表示順を `start_time` または `duration` で並び替える
   (デフォルトは `start_time` 昇順)。

出力は `[]*domain.Span`(フィルタ後の新しいroot群、または単一root)とする。

---

## 6. レイアウト層 (`layout` パッケージ)

### 6.1 Row構造

ツリーをフラットな行リストに変換する。**これが左右ペイン共通の Single Source of
Truth となる**(ツリー行とバー行がインデックスでずれることがないようにするため)。

```go
package layout

type Row struct {
	Span        *domain.Span
	Depth       int
	IsLast      bool // 兄弟内で最後の子か(ツリー描画の罫線用)
	StartCol    int  // バー開始位置(文字単位)
	EndCol      int  // バー終了位置(文字単位)
	HiddenChildren int // max-depthで隠れた子の数(0なら非表示)
}

type Layout struct {
	Rows       []Row
	TotalWidth int    // 全体の文字幅
	TreeWidth  int    // 左側ツリーペインの幅
	BarWidth   int    // 右側タイムラインペインの幅
	ViewStart  uint64 // 表示対象の開始時刻(nanos)
	ViewEnd    uint64 // 表示対象の終了時刻(nanos)
}
```

### 6.2 時間軸→列変換

```
ViewStart = フィルタ後の全spanの MinStart
ViewEnd   = フィルタ後の全spanの MaxEnd
duration  = ViewEnd - ViewStart (0除算を避けるため最小1に丸める)

col(t) = round( (t - ViewStart) / duration * BarWidth )

StartCol = col(span.StartNanos)
EndCol   = max(StartCol + 1, col(span.EndNanos))  // 幅0のバーを防ぐため最低1文字確保
```

### 6.3 ペイン幅の決定

```
TotalWidth = --width指定値 or 端末幅検出値 or 120(フォールバック)
TreeWidth  = min(TotalWidth * 0.4, 最長span名+depth*2 + 4)  // 上限あり、最大40%
BarWidth   = TotalWidth - TreeWidth - 1 (区切り文字分)
```

TreeWidthを超える長さのspan名は末尾を `...` で切り詰める。

### 6.4 ツリーの罫線情報

`IsLast` を使い、renderパッケージで `├─` / `└─` / `│ ` / `  ` を組み立てられるようにする。
これはUnicode box-drawing文字を使うが、`--color=never`相当の環境でも文字自体は
崩れないため、ASCII代替(`+--`, `|`)へのフォールバックオプションは持たない
(box-drawing文字はUTF-8端末なら広くサポートされているため)。

---

## 7. 描画層 (`render` パッケージ)

### 7.1 出力フォーマット例

```
Trace: 4bf92f3577b34da6a3ce929d0e0e4736  (root: HTTP GET /api/orders, duration: 842ms)

span                              0ms                          421ms                        842ms
├─ HTTP GET /api/orders           ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓  842ms
│  ├─ gateway.route                  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓         801ms
│  │  ├─ auth-service.verify              ▓▓▓▓▓▓▓                                                52ms
│  │  └─ db-query [ERROR]                        ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓   612ms
│  └─ ... (3 children hidden by --max-depth)
```

- ヘッダ行: トレースID、root span名、全体duration
- 目盛り行: ViewStartからViewEndまでを3〜5等分した時刻ラベル
- 各行: ツリー罫線 + span名 (+ status異常なら `[ERROR]`) + タイムラインバー + duration
- `--show-attributes` 指定時は、span名の後ろに `(http.status_code=500)` の形式で追記
- durationの単位は `--time-unit` に従う。`auto` の場合は全体durationに応じて
  ms/us/nsを自動選択(全体が1ms未満ならus単位、など)。

### 7.2 色付け仕様 (`--color`)

| 対象 | 色 |
|---|---|
| status=ERROR のspan名・バー | 赤 |
| status=OK | デフォルト色(無着色) |
| status=UNSET | グレー(dim) |
| 目盛り行 | dim |

`--color=auto` の場合、`os.Stdout` が端末(TTY)かどうかを判定し、パイプ/
リダイレクト時は自動的に無色にする。判定には `golang.org/x/term.IsTerminal` を使う。
`--color=always` / `never` で明示上書き可能。

### 7.3 端末幅の検出

`--width` 未指定時、`golang.org/x/term.GetSize(int(os.Stdout.Fd()))` を試み、
失敗(非TTY等)した場合は `120` にフォールバックする。

---

## 8. 主要な依存ライブラリ(Go)

| 用途 | ライブラリ | 備考 |
|---|---|---|
| CLIフラグ | `spf13/pflag` または標準 `flag` | サブコマンド不要なため標準`flag`でも十分 |
| YAML設定 | `gopkg.in/yaml.v3` | |
| 端末幅・TTY判定 | `golang.org/x/term` | |
| ANSIカラー | `fatih/color` または `charmbracelet/lipgloss` | スタイリングのみ、対話UIは使わない |
| JSON | 標準 `encoding/json` | uint64のカスタムUnmarshalerを自前実装 |

**あえて使わないもの**: `bubbletea` 等の対話TUIフレームワーク
(このツールはループを持たないため不要)。

---

## 9. エラーハンドリング方針

| 状況 | 挙動 |
|---|---|
| 入力ファイルが存在しない/読めない | stderrにエラー出力、exit code 1 |
| JSON全体が不正 | stderrにエラー出力、exit code 1 |
| 個別spanのフィールド欠損 | stderrに警告、そのspanをスキップして継続 |
| `--trace-id` 指定が存在しない | stderrにエラー、exit code 1 |
| `--root-span-id`/`--root-span-name` が見つからない | stderrにエラー、exit code 1 |
| 複数traceId存在 かつ `--trace-id` 未指定 | stderrに警告(どれを選んだか明示)、処理は継続 |
| span数0件(フィルタ後) | "no spans to display" をstdoutに出し、exit code 0 |

exit codeは将来のCI組み込み(例: 「ERROR spanがあれば非ゼロ終了」)を見据え、
`--fail-on-error` フラグ(v2検討)で拡張できるようにしておく。

---

## 10. テスト戦略

- `otlp`パッケージ: 実際のOTLP/JSONサンプル(正常系・欠損parentSpanId・複数trace混在・
  文字列/数値混在のtimestamp)を fixture として用意し、変換結果をテーブルテストで検証。
- `filter`パッケージ: root解決・max-depth・top-nの組み合わせを単体でテスト
  (親を強制的に残すロジックは特に念入りに)。
- `layout`パッケージ: 時間軸→列変換の境界値(duration=0、span数1件、幅0など)を検証。
- `render`パッケージ: 出力文字列のゴールデンファイルテスト(fixture入力→期待出力の
  完全一致比較)。色付き/無色それぞれのゴールデンファイルを用意する。
- E2E: `otel-oneshot testdata/sample.json --root-span-name=... ` をexec.Commandで
  実行し、標準出力を既知の期待値と比較。

---

## 11. 実装順序(推奨)

1. `domain` の型定義 + `otlp` パーサー(uint64対応含む) + ツリー構築
2. `filter`(root解決のみ、max-depth/top-nは後回し)
3. `layout`(時間軸変換)
4. `render`(色なし、罫線のみの最小出力)
5. CLIフラグ/YAML設定のマージ
6. `--max-depth` / `--top-n` / `--show-attributes` / 色付け / 単位自動選択を追加
7. ゴールデンファイルテスト整備

最小構成(1〜4)が動いた時点で「1トレースを読み込んでツリーとバーを出す」という
コア価値は完成する。それ以降は独立して追加可能なオプション機能なので、
段階的に他のAIへ実装依頼を分割することもできる。