---
name: otel-oneshot
description: Render an OTLP/JSON trace file as an ASCII span timeline using this repo's otel-oneshot CLI. Use when the user wants to visualize, inspect, or debug a trace (OpenTelemetry OTLP JSON) from the terminal — e.g. "show this trace", "why is this slow", "which span errored", "visualize spans", pointing at a .json trace/dump. Not for live/gRPC collection or multi-file merges.
---

# otel-oneshot

`otel-oneshot` は 1 つの OTLP/JSON ファイルを受け取り、span ツリーとタイムラインバーを
ASCII で標準出力に **1 回だけ** 描画する非対話 CLI です。このスキルは、ユーザーの意図を
適切なフラグに変換して実行するためのガイドです。

## 実行方法

リポジトリルート(`otel-oneshot`)から:

```sh
go run . <input.json> [flags]
```

繰り返し使う場合はビルドしておくと速いです:

```sh
go build -o otel-oneshot . && ./otel-oneshot <input.json> [flags]
```

- 入力パスを省略/`-` で **stdin** から読む。フラグと入力パスは順不同。
- 出力を人間に見せるとき(パイプ経由でツール出力を渡すと TTY 判定が効かないため)は
  **`--width` を明示**し、色コードを避けたいなら **`--color never`** を付ける。
  例: `go run . trace.json --width 120 --color never`

## ユーザー意図 → フラグ 対応表

| ユーザーが言うこと | 使うフラグ |
|---|---|
| 「このトレースを見せて / 可視化して」 | (フラグなし)。パイプ表示なら `--width 120 --color never` |
| 「遅い span はどれ」「ボトルネックは」 | `--top-n 10 --sort duration` |
| 「◯◯ より下だけ見たい」 | `--root-span-name "<span名>"` または `--root-span-id <id>` |
| 「深すぎる、浅くして」 | `--max-depth 3`(数字は適宜) |
| 「フレームワーク/gem のノイズを消して」 | `--hide '<正規表現>'`(繰り返し可)。マッチ span を消し子を親に繋ぎ直す。例: `--hide '^Sinatra::' --hide '^Rack::'` |
| 「この subtree は畳んでおいて」 | `--fold '<正規表現>'`(繰り返し可)。span は残し子孫を隠す |
| 「完全一致で消したい/正規表現メタ文字を含む名前」 | `--match-mode exact`(既定は `regex`)。例: `--hide 'Hash#[]' --match-mode exact` |
| 「HTTP ステータスや SQL も出して」 | `--show-attributes http.status_code,db.statement` |
| 「ナノ/マイクロ秒まで細かく」 | `--time-unit ns`(または `us`) |
| 「エラーだけ目立たせて」 | 既定で ON。抑止は `--highlight-errors=false` |
| 「ファイルに複数トレースある」 | まず既定実行 → stderr の警告で trace 一覧を確認 → `--trace-id <id>` |

## 実行後の読み解き方(ユーザーへの説明の要点)

- `[ERROR]` タグと赤色 = `status=ERROR` の span。**まずここを疑う**。
- バーの開始位置 = 開始時刻、長さ = duration。**子が親の終盤に集中/直列に並ぶ**なら
  そこが待ち時間の主因。
- 右端の数値が各 span の duration。ヘッダの `duration:` が全体。
- `auto` 単位は全体長で単位を選ぶため、極短 span が全部 `1us` に丸まることがある。
  細かく見たいときは `--time-unit ns` を勧める。

## よくある落とし穴

- **引数エラー「expected at most one input file」** は解決済み(順不同対応)。もし出たら
  ビルドが古い可能性 → 再ビルド。
- パイプ先で **色コード(`\x1b[...`)が混じる** → `--color never` を付ける。
- **表示が潰れる/折り返す** → `--width` を端末幅に合わせて明示。
- ファイルが巨大/深い → `--max-depth` と `--top-n` で絞ってから見る。

## スコープ外(このツールでやらないこと)

- gRPC/HTTP コレクタとしてのライブ受信、複数ファイルのマージ、対話ズーム/スクロール。
  これらを求められたら、このツールの守備範囲外であることを伝える。

詳細フラグは README.md、内部設計は DESIGN.md を参照。
