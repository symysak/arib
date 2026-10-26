# arib

ARIB 標準規格のデジタル無線の受信処理を実装した検証用・研究用のソフトウェア。

> このリポジトリを参考に何か作ったときは、どこかに次の 1 行を書いてもらえるとうれしい。
>
> `Based on symysak/arib — https://github.com/symysak/arib`

## 法的注意

使う前に [LEGAL.md](LEGAL.md) を読むこと。受信を始めるときに一度だけ同意を求める。

特定の相手方に向けた無線通信の存在や内容を漏らすこと・窃用することは、電波法 第 59 条で禁じられている。
受信したものを公開・配信しない、Web モニタを外へ開かない、記録を人に渡さない。

## 実装の範囲

| 規格 | 対象 | バイナリ | 音声符号 |
|---|---|---|---|
| STD-T86 | 市町村デジタル同報通信システム（60MHz 帯、16QAM） | `std-t86` | G.722.1（同梱） |
| STD-T115 QPSK ナロー方式 | 市町村デジタル同報通信システム（60MHz 帯、QPSK） | `std-t115-qpsknarrow` | AMR-WB+ |
| STD-T79 TYPE1 | 都道府県・市町村デジタル移動通信システム（260MHz 帯） | `std-t79` | AMBE+ 4000/2400 の網だけ |
| STD-T61 SCPC/FDMA | 狭帯域デジタル通信方式 | `std-t61` | なし（レイヤ 1〜3 まで） |

`arib` は全部入り。`arib live --std t86|t115-qpsknarrow|t79|t61 …` は各バイナリの `live` と同じで、
`arib probe` は 1 局を全部の規格で同時に試して、どれかを判定する。

## 入手

`std-t86` と `std-t115-qpsknarrow` は [Releases](https://github.com/symysak/arib/releases/latest) の zip
（`<バイナリ名>-<バージョン>-<OS>-<CPU>.zip`、チェックサムは `SHA256SUMS.txt`）。
`build/` は実行ファイルの隣に置いたままにする（音声復号器をそこから探す）。
AMR-WB+ は再頒布できないので、zip の中の `build_amrwbplus.sh` / `.ps1` でビルドする。

`std-t79`・`std-t61`・`arib` は配っていない。ソースからビルドする（Go、C コンパイラ、git が要る）。

```sh
go build -o std-t86             ./cmd/std-t86
go build -o std-t115-qpsknarrow ./cmd/std-t115/qpsknarrow
go build -o std-t79             ./cmd/std-t79
go build -o std-t61             ./cmd/std-t61
go build -o arib                ./cmd/arib

bash scripts/std-t86/build_g7221.sh        # G.722.1
bash scripts/std-t115/build_amrwbplus.sh   # AMR-WB+
bash scripts/std-t79/build_mbelib_neo.sh   # AMBE+ 4000/2400（mbelib-neo）
```

Windows は同じ名前の `.ps1`。

## 実行方法

```sh
./std-t86 live -input sdrsharp://127.0.0.1:5555
./std-t86 live -scan -input rtltcp://127.0.0.1:1234@60.05,gain=40
./std-t86 live -channels 60.025,60.075 -input rtltcp://127.0.0.1:1234@60.05
./arib probe -input sdrsharp://127.0.0.1:5555
```

最初の 1 回は法的注意への同意を聞く（`yes` か `-accept-notice`）。

入力は `-input` で渡す。何度でも書けて、2 本目以降の SDR で帯域を広げる。

| 入力 | 書き方 |
|---|---|
| SDR#（`sdrsharp-plugin/` のプラグイン） | `sdrsharp://127.0.0.1:5555` |
| rtl_tcp | `rtltcp://127.0.0.1:1234@<中心 MHz>,gain=<dB>` |
| 生の I/Q の TCP | `tcp://127.0.0.1:5555@<中心 MHz>,fs=<Hz>,fmt=s16` |
| 録音（`.wav` は I=L / Q=R、`.cu8`、`.cf32`） | `<ファイル>`（名前に `…_<MHz>MHz` が無ければ `@<中心 MHz>`） |

`-channels` も `-scan` も無ければ、入力の中心を 1 局として受ける。STD-T79 は 2 本目の入力に上りの帯を渡すと上りも受ける。

Web モニタは `std-t86` / `std-t115-qpsknarrow` が http://127.0.0.1:8000/ 、`std-t79` が :8130、`std-t61` が :8080、`arib probe` が :8200。
既定の待ち受けは `127.0.0.1`。

記録は `-log-dir`（既定 `logs`）の下:

```
logs/<バイナリ名>/
  watch.log         受信のログ
  ledger/           見つけた局の台帳
  <YYYY-MM-DD>/<HHMMSS>_<周波数>MHz.iq.wav    I/Q（入力にして再解析できる）
                                   .txt       解析の結果
                                   .audio.wav 音声
```

| オプション | 意味 |
|---|---|
| `-input` | 入力。`<入力>[@<中心 MHz>][,gain=<dB>][,agc][,fs=<Hz>][,fmt=cf32\|s16\|cu8][,scan\|noscan][,label=<名前>]` |
| `-scan` | 帯域を見張って局を足す |
| `-channels` | 常に見張る局 [MHz]。`none` で 0 本 |
| `-addr` | Web モニタの待ち受け |
| `-log-dir` | 記録の置き場所 |
| `-ledger` | 台帳の置き場所（`none` で使わない） |
| `-full-speed` | 録音を全速で処理する |
| `-accept-notice` | 法的注意に同意する |

全部は `<バイナリ> live -h`。

## ライセンス

(c) 2026 symysak

`third_party/` の ITU-T G.722.1 参照実装など第三者のものは、それぞれの条件に従う。実行ファイルに
組み込まれている Go と WebSocket ライブラリの表示も含めて `THIRD_PARTY_NOTICES.md` にまとめてある。

## 作成方法と限界

全てのコードは Claude Code により生成した。拠り所はインターネット上の公開情報と実電波だけで、
規格書にない部分は実波からの推定である。そのため誤りを多く含むと考えられる。

## 参考文献
- 市町村デジタル同報通信システムの規格等に関する調査検討報告書  
https://warp.ndl.go.jp/web/20090113093911/http://www.soumu.go.jp/s-news/2003/pdf/030425_2_03.pdf
- DIGITAL SIMULTANEOUS COMMUNICATION SYSTEMS FOR MUNICIPALITIES GOVERNMENT TYPE2  
https://www.arib.or.jp/english/html/overview/doc/5-STD-T115v2_7-E1.pdf
