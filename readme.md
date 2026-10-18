# arib — ARIB 規格のデジタル無線 受信デコーダ

日本の ARIB 標準規格で定められたデジタル無線を SDR で受信して、**ブラウザで見て聴く**ための OSS デコーダ。
同報無線（市町村デジタル同報通信システム）から自治体の移動系の無線（都道府県・市町村デジタル移動通信システム）まで、
規格ごとの復号器を同じ受信エンジンと画面の上に載せている。

> このリポジトリを参考に何か作ったときは、どこかに次の 1 行を書いてもらえるとうれしいです。
>
> `Based on symysak/arib — https://github.com/symysak/arib`

## 対応している規格

| 規格 | 使われているところ | 変調 | バイナリ | 音声 |
|---|---|---|---|---|
| ARIB STD-T86 | 市町村デジタル同報通信システム（60MHz 帯） | 16QAM TDMA-TDD | `std-t86` | 鳴る（G.722.1、同梱） |
| ARIB STD-T115 QPSK ナロー方式 | 市町村デジタル同報通信システム（60MHz 帯） | QPSK SCPC（7.5kHz 間隔） | `std-t115-qpsknarrow` | 鳴る（AMR-WB+、各自ビルド） |
| ARIB STD-T79 TYPE1 | 都道府県・市町村デジタル移動通信システム（260MHz 帯） | π/4 シフト QPSK 4 スロット TDMA（25kHz 間隔） | `std-t79` | AMBE+ 4000/2400 の網は鳴る（mbelib-neo、各自ビルド。macOS / Linux のみ）。ほかの符号器の網は鳴らない |

- STD-T86 と STD-T115 は同じ同報無線の別の規格。自分の地域がどちらかは、受信して同期するほうを使えばよい。
- STD-T79 は物理層からレイヤ 3 までの規格で、`std-t79` は 4 スロットの同期・制御チャネルとレイヤ 1〜3 の解読・帯域の走査・
  スロットごとの通話の録音までを行う（2 台目の SDR で上りも受けられる）。音声の符号化方式は規格が決めておらず網ごとに違う。
  鳴らせるのは **AMBE+ 4000/2400 の網**だけ（既定の `-codec auto` が伝送路の検査で選ぶ）。音声デコーダ mbelib-neo を各自ビルドする（手順 2）。
  mbelib-neo の音声の表は近似なので、音質は本物の機器より落ちる。ほかの符号器の網は鳴らない（解読と録音だけ）。

どのバイナリも使い方は同じ（コマンド名と一部のオプションだけが違う）。以下の手順で動かす。

1. [ダウンロードする](#1-ダウンロードする)
2. [音声デコーダを用意する](#2-音声デコーダを用意する)（STD-T115・STD-T79 だけ）
3. [SDR 側を待ち受けにする](#3-sdr-側を待ち受けにする)
4. [起動する](#4-起動する)
5. [ブラウザで見て聴く](#5-ブラウザで見て聴く)

## 1. ダウンロードする

**ビルドは要らない。** [Releases](https://github.com/symysak/arib/releases/latest) から、
使うバイナリと OS / CPU に合う zip を落として展開するだけで動く（Go も C コンパイラも不要）。

zip 名は `<バイナリ名>-<バージョン>-<OS>-<CPU>.zip`（バイナリ名は `std-t86` / `std-t115-qpsknarrow` / `std-t79`）。

| OS / CPU | zip の末尾 |
|---|---|
| Windows (x64) | `…-windows-amd64.zip` |
| macOS (Apple Silicon) | `…-darwin-arm64.zip` |
| macOS (Intel) | `…-darwin-amd64.zip` |
| Linux (x86_64) | `…-linux-amd64.zip` |
| Linux (aarch64) | `…-linux-arm64.zip` |

Windows on ARM 向けは配布していない（x64 版がエミュレーションで動く）。
チェックサムは同じページの `SHA256SUMS.txt`。

展開したフォルダの中身:

| | 入っている zip | 中身 |
|---|---|---|
| `std-t86` などの本体（Windows は `.exe`） | 全部 | Web モニタの画面と市区町村コード表はバイナリに埋め込んであるので、これ 1 個で動く |
| `build/g7221/` | `std-t86` | STD-T86 の音声デコーダ（ITU-T G.722.1） |
| `build_amrwbplus.ps1` / `build_amrwbplus.sh` | `std-t115-qpsknarrow` | STD-T115 の音声デコーダを作るスクリプト（手順 2） |
| `build_mbelib_neo.ps1` / `build_mbelib_neo.sh` | `std-t79` | STD-T79 の AMBE+ の音声デコーダ（mbelib-neo）を作るスクリプト（手順 2） |
| `sdrsharp-plugin/` | 全部（Windows 版のみ） | SDR# から I/Q を流すプラグイン（手順 3） |
| `readme.md` / `THIRD_PARTY_NOTICES.md` | 全部 | これと、第三者のものの表示 |

**`build/` は実行ファイルと同じ場所に置いたままにすること。** 音声デコーダは実行ファイルからの相対位置で
探すので、動かしたり消したりすると音だけ出なくなる（受信と制御チャネルの復号は動く）。

### macOS は初回だけ検疫属性を外す

署名していないので、ダウンロードした zip には Gatekeeper の検疫属性が付く。

```sh
xattr -dr com.apple.quarantine <展開したフォルダ>
```

## 2. 音声デコーダを用意する

| バイナリ | すること |
|---|---|
| `std-t86` | 何もしない（G.722.1 が zip に入っている） |
| `std-t115-qpsknarrow` | 同梱のスクリプトを 1 回走らせる（下記） |
| `std-t79` | AMBE+ の網を聴くなら同梱のスクリプトを 1 回走らせる（下記） |

STD-T115 の音声（AMR-WB+、3GPP TS 26.304）は、3GPP の配布物を書面の許可なく再頒布できないので zip に入れていない。
代わりに、取得・パッチ・ビルドまでを行うスクリプトが入っている。**展開したフォルダの中でそのまま実行するだけ**でよい
（出力先は自動で実行ファイルの隣の `build/amrwbplus/` になる）。

- **Windows** — `build_amrwbplus.ps1` を右クリック →「PowerShell で実行」。gcc が無ければ MinGW-w64（WinLibs）を
  winget で入れる（winget が使えなければ WinLibs を直接ダウンロードして `build\toolchain\` へ展開する）。
  管理者権限も Python も要らない。
- **macOS / Linux** — C コンパイラ（macOS は `xcode-select --install`、Linux は `build-essential` 等）と curl・unzip が要る。

  ```sh
  bash ./build_amrwbplus.sh
  ```

数分で `build/amrwbplus/amrwbp_decoder` が出来る。デコーダを起動し直せば音が鳴る。

STD-T79 の AMBE+ 4000/2400 は、音声デコーダ mbelib-neo（GPL-2.0-or-later）を使う。zip には入れず、取得してビルドする
スクリプトが入っている。**展開したフォルダの中でそのまま実行するだけ**でよい（出力先は実行ファイルの隣の `build/mbelib-neo/`）。

- **Windows** — `build_mbelib_neo.ps1` を右クリック →「PowerShell で実行」。gcc が無ければ MinGW-w64（WinLibs）を
  入れるところまでやる（`build_amrwbplus.ps1` と同じ）。git も管理者権限も要らない。
- **macOS / Linux** — C コンパイラ（macOS は `xcode-select --install`、Linux は `build-essential` 等）と git が要る。

  ```sh
  bash ./build_mbelib_neo.sh
  ```

`build/mbelib-neo/ambeplus_decode`（Windows は `.exe`）が出来る。

## 3. SDR 側を待ち受けにする

デコーダは **TCP クライアント**として繋ぎにいくので、SDR 側をサーバにしておく。

| SDR | 準備 | デコーダの入力 |
|---|---|---|
| SDR#（Windows） | 同梱のプラグイン（下記） | `-input sdrsharp://127.0.0.1:5555`（レート・形式・周波数は接続の中で伝わる） |
| SDR++ | IQ Exporter（Network Sink）を TCP / Int16 で待ち受けにする | `-input tcp://127.0.0.1:5555@<MHz>,fs=<サンプルレート>,fmt=s16` |
| rtl_tcp | `rtl_tcp -a 0.0.0.0` で起動しておく | `-input rtltcp://127.0.0.1:1234@<MHz>,gain=<dB>`（同調・利得・レートはデコーダから。レートの既定は 2.4 MS/s、変えるなら `,fs=`） |

**SDR# のプラグイン**: zip の `sdrsharp-plugin\SDRSharp.IqTcpServer.dll` を SDR# の `Plugins\` フォルダへコピーし、
**`SDRSharp.dotnet9.exe`（.NET 9 ホスト）**で SDR# を起動すると、右ペインに **"IQ TCP Server (sdrsharp://)"** が出る
（詳細は `sdrsharp-plugin\README.md`）。受信したいチャネルへ同調して再生（▶）→ **Start server**。
プラグインは選択中の VFO を 0 Hz へ落として間引いてから流すので、周波数を変えたいときは SDR# のスペクトラムを
クリックするだけでよい（デコーダは VFO に付いて行く。再起動は要らない）。

録音ファイル（`.wav` は I=L / Q=R、`.cu8`、`.cf32`）もそのまま入力にできる（`.wav` 以外は `,fs=` が要る）。

## 4. 起動する

形はどのバイナリも同じ: **`<バイナリ> live [オプション] -input <入力>[@<中心 MHz>][,gain=<dB>]…`**。
入力は `-input` だけで書く。中心周波数・利得・サンプルレート・形式は `-input` の中に書く（下の「オプション」）。

```sh
# SDR#
./std-t86             live -input sdrsharp://127.0.0.1:5555
./std-t115-qpsknarrow live -input sdrsharp://127.0.0.1:5555

# SDR++（Int16）
./std-t86             live -input tcp://127.0.0.1:5555@58.5,fs=192000,fmt=s16

# rtl_tcp
./std-t115-qpsknarrow live -input rtltcp://127.0.0.1:1234@60.0,fs=1024000,gain=40

# 録音ファイル（周波数はファイル名の …_<Hz>Hz / …_<MHz>MHz から読めれば @<MHz> は要らない）
./std-t86             live -input recording.wav
```

Windows は `std-t86.exe` などをコマンドプロンプトか PowerShell から起動する（エクスプローラでダブルクリックしても
引数を渡せない）。周波数はどのバイナリも **MHz**。

### 複数の局を同時に受ける

広帯域の入力（rtl_tcp・広帯域の録音）なら、1 本の入力から何局でも同時に受けられる。
`-input` の `@` に入力の中心、`-channels` に局を並べる。`-scan` を付けると帯域を見張って、見つけた局を自動で足す。
SDR を 2 本以上つなぐなら `-input` を並べる（1 本目が主の入力。局は帯域に入る入力で受ける）。
局は画面の「設定」から足し引きもできる（`-channels none` なら局 0 本で起動して、画面から足す）。

```sh
./std-t86             live -channels 58.445,58.505 -input rtltcp://127.0.0.1:1234@58.5,fs=1024000
./std-t115-qpsknarrow live -scan -input rtltcp://127.0.0.1:1234@60.0,fs=1024000
./std-t79             live -scan -input rtltcp://127.0.0.1:1234@<中心1>,gain=40 -input rtltcp://127.0.0.1:1235@<中心2>,gain=20   # SDR 2 本
```

局ごとにスクランブル値を固定するときは、局の後ろに付ける（ふだんは自動判定するので要らない）:

| 規格 | 書き方 |
|---|---|
| STD-T86 | `58.505:seed=289`（`0..511`、`0` も有効な値）または `58.505:muni=<市区町村コード>` |
| STD-T115 | `60.0075:scramble=0x1234`（`0x` 付きは 16 進） |

### STD-T79 を受ける

STD-T79 の網は制御キャリアも通信キャリアも常時送信なので、**帯域を走査して局を見つける**のが普通の使い方
（1 局 = 1 キャリア、中に 4 スロット）。通話はスロットごとに録音する。スクランブルのパターンは無線チャネル指定から読むか、受信しながら当てる。
2 本目の `-input` に上りの帯の SDR を渡すと上り（移動局の送信）も受ける（中心を省くと 1 本目の中心 − 9 MHz）。

```sh
./std-t79 live -scan -input rtltcp://127.0.0.1:1234@<帯域の中心 MHz>
./std-t79 live -scan -input rtltcp://127.0.0.1:1234@<帯域の中心 MHz> -input rtltcp://127.0.0.1:1235,gain=20   # 下りと上り
./std-t79 recording.wav     # live を使わず、スロットとメッセージを端末に一覧する
```

見つけた局は台帳（`logs/std-t79/ledger/`）に残り、次の起動でも監視する。

## 5. ブラウザで見て聴く

起動したら画面を開く。

| バイナリ | 画面 |
|---|---|
| `std-t86` / `std-t115-qpsknarrow` | http://127.0.0.1:8000/ |
| `std-t79` | http://127.0.0.1:8130/ |

受信できていれば同期がかかる。同報無線なら、放送が始まると通報として検出される。
音声ペインの **「再生開始」ボタン**を押すと鳴りはじめる（ブラウザの自動再生制限があるので、最初の 1 回はクリックが要る）。
録音（I/Q・音声・記録）は画面の録音の一覧から取り出せる。

`std-t86` と `std-t115-qpsknarrow` はどちらも 8000 で待ち受けるので、同時に動かすときはどちらかを `-addr 127.0.0.1:8001` などへずらすこと。

## 記録の置き場所

`-log-dir`（既定 `logs`）の下に、バイナリごとのフォルダを作って置く。

```
logs/<バイナリ名>/
  watch.log                     受信のログ
  ledger/                       見つけた局の台帳（次の起動へ持ち越す）
  <YYYY-MM-DD>/<HHMMSS>_<周波数>MHz[_<ラベル>].iq.wav     I/Q（そのまま入力にして再デコードできる）
                                               .txt        記録（何を受けたか）
                                               .audio.wav  音声
```

## オプション

全部のバイナリで同じ名前・同じ意味。

| オプション | 意味 |
|---|---|
| `-input` | 入力（何度でも。1 本目が主の入力）: `<入力>[@<中心 MHz>][,gain=<dB>][,agc][,fs=<Hz>][,fmt=<形式>][,scan\|noscan][,label=<名前>]`。下の表 |
| `-channels` | 局 [MHz]（カンマ区切り）。局ごとの設定は `<MHz>:<鍵>=<値>`。省略で入力の中心の 1 局、`none` で局 0 本 |
| `-scan` | 帯域を見張って局を自動で足す |
| `-addr` | 画面の待ち受け（既定は上の表） |
| `-log-dir` | 記録の置き場所（既定 `logs`） |
| `-ledger` | 台帳の置き場所（既定 `<log-dir>/<バイナリ名>/ledger`。`none` で使わない） |
| `-full-speed` | 録音ファイルを実時間ではなく全速で処理する（`-speed` で倍率も変えられる） |

`-input` の中に書くもの:

| 書き方 | 意味 |
|---|---|
| `@<中心 MHz>` | 入力の中心周波数。`-channels` / `-scan` が無く入力が 1 本なら受信する局。rtl_tcp は同調も。`sdrsharp://` と名前に周波数のある録音では省ける |
| `,gain=<dB>` / `,agc` | rtl_tcp の利得 / 受信機の AGC（書かなければバイナリごとの既定） |
| `,fs=<Hz>` | サンプルレート（`tcp://` と `.wav` 以外の録音では必須。rtl_tcp は既定 2.4 MS/s、`sdrsharp://` と `.wav` では不要） |
| `,fmt=<形式>` | `tcp://` の標本形式。`cf32`（既定）/ `s16`（SDR++ の Int16）/ `cu8` |
| `,scan` / `,noscan` | その入力だけ走査する / しない（書かなければ `-scan` に倣う） |
| `,label=<名前>` | 入力の名前（画面とログに出る） |

全部のオプションは `<バイナリ> live -h` で出る。

## 音が出ないとき

1. **音声デコーダがあるか**。`build/` を実行ファイルの隣に置いたままか（手順 1）、STD-T115 はスクリプトを
   走らせたか（手順 2）。無いと画面のログに警告が出る。STD-T79 は AMBE+ の網（`build_mbelib_neo` のスクリプトを走らせたとき）だけが鳴る（ほかの符号器の網はもともと鳴らない。鳴ったときは録音の隣に `.audio.wav` と、選んだ符号器を書いた `.calls.txt` ができる）。
2. **「再生開始」を押したか**。ブラウザは操作なしに音を鳴らせない。
3. **同期しているか**。画面の CRC 一致率が 0 のままなら、チャネルに同調できていないか、
   （`tcp://` のとき）`-fs` が SDR 側の送出レートと合っていないか `-fmt` の形式が違う。
4. **電波が出ていない**。同報無線は常時送信ではないので、放送が無い時間帯は制御信号すら
   出ないことがある（STD-T115 は待機中は電波が出ない）。

## ソースからビルドする

配布していない OS / CPU で動かしたいときや、自分で改造するとき。Go が要る。

```sh
# デコーダ本体（使うものだけでよい）
go build -o std-t86             ./cmd/std-t86
go build -o std-t115-qpsknarrow ./cmd/std-t115/qpsknarrow
go build -o std-t79             ./cmd/std-t79

# 音声デコーダ（使う規格のものだけでよい。出力は build/ の下）
bash scripts/std-t86/build_g7221.sh        # STD-T86 用（G.722.1）
bash scripts/std-t115/build_amrwbplus.sh   # STD-T115 用（AMR-WB+）
bash scripts/std-t79/build_mbelib_neo.sh   # STD-T79 用（AMBE+ 4000/2400、mbelib-neo）
```

Windows は `pwsh scripts/std-t86/build_g7221.ps1` / `pwsh scripts/std-t115/build_amrwbplus.ps1` / `pwsh scripts/std-t79/build_mbelib_neo.ps1`。
MSVC の `cl` は非対応（gcc 系が要る）だが、どちらのスクリプトも gcc が無ければ自分で MinGW-w64 を用意するので、
用意しておくのは Go だけでよい。

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
