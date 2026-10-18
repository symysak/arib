# arib — ARIB 規格のデジタル無線 受信デコーダ

ARIB 標準規格のデジタル無線を SDR で受信して、ブラウザで見て聴くためのデコーダ。

> このリポジトリを参考に何か作ったときは、どこかに次の 1 行を書いてもらえるとうれしいです。
>
> `Based on symysak/arib — https://github.com/symysak/arib`

## 対応している規格

| 規格 | 対象 | バイナリ | 音声 |
|---|---|---|---|
| ARIB STD-T86 | 市町村デジタル同報通信システム（60MHz 帯、16QAM） | `std-t86` | G.722.1（同梱） |
| ARIB STD-T115 QPSK ナロー方式 | 市町村デジタル同報通信システム（60MHz 帯、QPSK） | `std-t115-qpsknarrow` | AMR-WB+（各自ビルド） |
| ARIB STD-T79 TYPE1 | 都道府県・市町村デジタル移動通信システム（260MHz 帯） | `std-t79` | AMBE+ 4000/2400 の網だけ（各自ビルド）。ほかの網は解読と録音だけ |

同報無線の 2 規格は、自分の地域でどちらが使われているか分からなければ両方試して同期するほうを使う。

## 1. ダウンロードする

[Releases](https://github.com/symysak/arib/releases/latest) から `<バイナリ名>-<バージョン>-<OS>-<CPU>.zip` を落として展開する
（`windows-amd64` / `darwin-arm64` / `darwin-amd64` / `linux-amd64` / `linux-arm64`）。チェックサムは `SHA256SUMS.txt`。

zip には本体（画面は埋め込み済み）・音声デコーダ（`build/`）かそのビルドスクリプト・SDR# のプラグイン（Windows 版のみ）が入っている。
**`build/` は実行ファイルの隣から動かさないこと**（音声デコーダを実行ファイルからの相対位置で探す）。

macOS は署名していないので、初回だけ検疫属性を外す:

```sh
xattr -dr com.apple.quarantine <展開したフォルダ>
```

## 2. 音声デコーダを用意する（STD-T115・STD-T79）

STD-T86 は不要。STD-T115 の AMR-WB+ は再頒布できず、STD-T79 の mbelib-neo は GPL なので、zip にはビルドスクリプトだけを入れてある。
展開したフォルダでそのまま実行すれば、実行ファイルの隣の `build/` に出来る。

| | Windows（右クリック →「PowerShell で実行」） | macOS / Linux |
|---|---|---|
| STD-T115 | `build_amrwbplus.ps1` | `bash ./build_amrwbplus.sh`（C コンパイラ・curl・unzip が要る） |
| STD-T79 | `build_mbelib_neo.ps1` | `bash ./build_mbelib_neo.sh`（C コンパイラ・git が要る） |

Windows のスクリプトは gcc が無ければ MinGW-w64 を入れるところまでやる（管理者権限は要らない）。
mbelib-neo の音声の表は近似なので、STD-T79 の音質は本物の機器より落ちる。

## 3. SDR# を待ち受けにする

デコーダは TCP クライアントとして SDR に繋ぎにいく。ここでは SDR#（Windows）と同梱のプラグインを使う
（rtl_tcp などは[ほかの入力](#ほかの入力)）。

1. `sdrsharp-plugin\SDRSharp.IqTcpServer.dll` を SDR# の `Plugins\` フォルダへコピーし、**`SDRSharp.dotnet9.exe`** で起動する。
   右ペインに **"IQ TCP Server (sdrsharp://)"** が出る。
2. 受信したい帯域の真ん中へ同調し、サンプルレートを帯域が収まる値にする（RTL-SDR なら 2.4 MS/s で約 2 MHz 幅）。
3. パネルの **Follow VFO** を外して（帯域を丸ごと流す）、再生（▶）→ **Start server**。

サンプルレートと中心周波数は接続の中でデコーダに伝わる。SDR# で中心を動かせばデコーダも付いて行く。

## 4. 起動する

```sh
./std-t86             live -scan -input sdrsharp://127.0.0.1:5555
./std-t115-qpsknarrow live -scan -input sdrsharp://127.0.0.1:5555
./std-t79             live -scan -input sdrsharp://127.0.0.1:5555
```

`-scan` は帯域を見張り、見つけた局を自動で足す。見つけた局は台帳（`logs/<バイナリ名>/ledger/`）に残り、次の起動でも監視する。
Windows はコマンドプロンプトか PowerShell から `std-t86.exe` などを起動する。

決まった局を常に見張るなら `-channels` を足す（MHz、カンマ区切り）。局は画面の「設定」からも足し引きできる。

```sh
./std-t86 live -scan -channels 58.445,58.505 -input sdrsharp://127.0.0.1:5555
```

### 1 局だけを聴く

プラグインの Follow VFO を付けたまま（既定）にすると、SDR# で選んだチャネルだけが流れる。デコーダは `-scan` を付けずに起動する:

```sh
./std-t86 live -input sdrsharp://127.0.0.1:5555
```

SDR# のスペクトラムでチャネルをクリックすれば、デコーダの局もそこへ移る。

### ほかの入力

| 入力 | 書き方 |
|---|---|
| rtl_tcp（`rtl_tcp -a 0.0.0.0`） | `-input rtltcp://127.0.0.1:1234@<中心 MHz>,gain=<dB>` |
| SDR++ の IQ Exporter（TCP / Int16） | `-input tcp://127.0.0.1:5555@<中心 MHz>,fs=<サンプルレート>,fmt=s16` |
| 録音ファイル（`.wav` は I=L / Q=R、`.cu8`、`.cf32`） | `-input recording.wav`（名前に `…_<MHz>MHz` が無ければ `@<中心 MHz>`、`.wav` 以外は `,fs=` も） |

```sh
./std-t115-qpsknarrow live -scan -input rtltcp://127.0.0.1:1234@60.0,gain=40
```

SDR を 2 本以上つなぐなら `-input` を並べる。STD-T79 は 2 本目に上りの帯の SDR を渡すと上りも受ける（中心を省くと 1 本目 − 9 MHz）。

## 5. ブラウザで見て聴く

`std-t86` / `std-t115-qpsknarrow` は http://127.0.0.1:8000/ 、`std-t79` は http://127.0.0.1:8130/ 。
音声ペインの「再生開始」を押すと鳴る（ブラウザの自動再生制限のため、最初の 1 回はクリックが要る）。
録音は画面の一覧から取り出せる。`std-t86` と `std-t115-qpsknarrow` を同時に動かすなら片方を `-addr 127.0.0.1:8001` などへずらす。

記録は `-log-dir`（既定 `logs`）の下に置く:

```
logs/<バイナリ名>/
  watch.log         受信のログ
  ledger/           見つけた局の台帳
  <YYYY-MM-DD>/<HHMMSS>_<周波数>MHz.iq.wav    I/Q（入力にして再デコードできる）
                                   .txt       何を受けたか
                                   .audio.wav 音声
```

## オプション

全部のバイナリで共通。全部は `<バイナリ> live -h` で出る。

| オプション | 意味 |
|---|---|
| `-input` | 入力（何度でも書ける）。`<入力>[@<中心 MHz>][,gain=<dB>][,agc][,fs=<Hz>][,fmt=cf32\|s16\|cu8][,scan\|noscan][,label=<名前>]` |
| `-scan` | 帯域を見張って局を自動で足す |
| `-channels` | 常に見張る局 [MHz]。`none` で局 0 本（画面から足す） |
| `-addr` | 画面の待ち受け |
| `-log-dir` | 記録の置き場所（既定 `logs`） |
| `-ledger` | 台帳の置き場所（`none` で使わない） |
| `-full-speed` | 録音ファイルを実時間ではなく全速で処理する |

## 音が出ないとき

1. **音声デコーダが無い**。`build/` が実行ファイルの隣にあるか、STD-T115・STD-T79 はスクリプトを走らせたか（無いと画面のログに警告が出る）。
   STD-T79 は AMBE+ 4000/2400 の網しか鳴らない。
2. **「再生開始」を押していない**。
3. **同期していない**。画面の CRC 一致率が 0 のままなら、帯域に局が入っていないか、`tcp://` の `,fs=` / `,fmt=` が SDR 側と合っていない。
4. **電波が出ていない**。同報無線は放送が無いと電波を出さないことがある（STD-T115 は待機中は出さない）。

## ソースからビルドする

Go が要る。

```sh
go build -o std-t86             ./cmd/std-t86
go build -o std-t115-qpsknarrow ./cmd/std-t115/qpsknarrow
go build -o std-t79             ./cmd/std-t79

bash scripts/std-t86/build_g7221.sh        # 音声デコーダ（使うものだけ。Windows は同名の .ps1）
bash scripts/std-t115/build_amrwbplus.sh
bash scripts/std-t79/build_mbelib_neo.sh
```

## ライセンス

(c) 2026 symysak

`third_party/` の ITU-T G.722.1 参照実装など第三者のものはそれぞれの条件に従う。実行ファイルに
組み込まれている Go と WebSocket ライブラリの表示も含めて `THIRD_PARTY_NOTICES.md` にまとめてある。

## 本プログラムの作成方法

全てのコードは Claude Code により生成されている。また、インターネット上の公開情報と実電波のみを元に作成している。
そのため、誤りが多数あると思われる。

## 参考文献
- 市町村デジタル同報通信システムの規格等に関する調査検討報告書  
https://warp.ndl.go.jp/web/20090113093911/http://www.soumu.go.jp/s-news/2003/pdf/030425_2_03.pdf
- DIGITAL SIMULTANEOUS COMMUNICATION SYSTEMS FOR MUNICIPALITIES GOVERNMENT TYPE2  
https://www.arib.or.jp/english/html/overview/doc/5-STD-T115v2_7-E1.pdf
