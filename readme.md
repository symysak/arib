# 市町村デジタル同報通信システム 受信デコーダ

60MHz 帯の市町村デジタル同報通信システム（防災行政無線の親局 → 屋外子局）を、SDR で
受信して**ブラウザで放送を聴く**ための OSS デコーダ。

2 つの規格に対応していて、**それぞれ別のバイナリ**になっている。

| 規格 | 変調 | バイナリ |
|---|---|---|
| ARIB STD-T86 | 16QAM TDMA-TDD | `std-t86` |
| ARIB STD-T115 QPSK ナロー方式 | QPSK SCPC（7.5kHz 間隔） | `std-t115-qpsknarrow` |

自分の地域がどちらかは、受信して同期するほうを使えばよい。以下の手順はどちらも同じで、
コマンド名とオプションの書式（`--fs` / `-fs`）だけが違う。

## 1. ダウンロードする

**ビルドは要らない。** [Releases](https://github.com/symysak/arib/releases/latest) から
規格と OS / CPU に合う zip を落として展開するだけで動く（Go も C コンパイラも不要）。

zip 名は `<バイナリ名>-<バージョン>-<OS>-<CPU>.zip`。

| OS / CPU | zip |
|---|---|
| Windows (x64) | `std-t86-…-windows-amd64.zip` / `std-t115-qpsknarrow-…-windows-amd64.zip` |
| macOS (Apple Silicon) | `…-darwin-arm64.zip` |
| macOS (Intel) | `…-darwin-amd64.zip` |
| Linux (x86_64) | `…-linux-amd64.zip` |
| Linux (aarch64) | `…-linux-arm64.zip` |

Windows on ARM 向けは配布していない（x64 版がエミュレーションで動く）。
チェックサムは同じページの `SHA256SUMS.txt`。

展開したフォルダの中身:

- `std-t86` / `std-t115-qpsknarrow`（Windows は `.exe`）— 本体。Web モニタの画面と
  市区町村コード表はバイナリに埋め込んであるので、これ 1 個で動く
- `build/g7221/` — `std-t86` の zip のみ。音声デコーダ（ITU-T G.722.1）。
  **実行ファイルと同じ場所に置いたままにすること**（実行ファイルからの相対位置で探すので、
  消すと音声だけ出なくなる。受信と制御チャネルの復号は動く）
- `build_amrwbplus.ps1`（Windows）/ `build_amrwbplus.sh`（macOS・Linux）
  — `std-t115-qpsknarrow` の zip のみ。音声デコーダ（AMR-WB+）を作るスクリプト（下記）
- `sdrsharp-plugin/` — Windows 版のみ。SDR# から I/Q を TCP で流すプラグイン（手順 2）
- `readme.md` — これ

### macOS は初回だけ検疫属性を外す

署名していないので、ダウンロードした zip には Gatekeeper の検疫属性が付く。

```sh
xattr -dr com.apple.quarantine <展開したフォルダ>
```

### STD-T115 の音声だけは 1 回スクリプトを走らせる

`std-t115-qpsknarrow` の zip には **AMR-WB+（3GPP TS 26.304）を同梱していない**。3GPP の
配布物は書面の許可なく再頒布できないため。代わりに、取得・パッチ・ビルドまでを行う
スクリプトが zip に入っている。**展開したフォルダの中でそのまま実行するだけ**でよい
（出力先は自動で実行ファイルの隣になる）。

**Windows** — `build_amrwbplus.ps1` を右クリック →「PowerShell で実行」。
コンパイラが要らない: **gcc が無ければ MinGW-w64（WinLibs）を winget で入れる**
（winget が使えなければ WinLibs を直接ダウンロードして zip 内の `build\toolchain\` へ
展開する）。管理者権限も Python も要らない。

**macOS / Linux** — C コンパイラが要る（macOS は `xcode-select --install`、
Linux は `build-essential` 等）。あとは curl と unzip だけ。

```sh
bash ./build_amrwbplus.sh
```

どちらも数分かかり、終わると `build/amrwbplus/amrwbp_decoder` が出来る。あとは
デコーダを起動し直せば音が鳴る。**このフォルダは実行ファイルと同じ場所に置いたままにする**
（消しても受信と制御チャネルの復号は動くが、音だけ出なくなる）。

`std-t86` の音声（G.722.1）は zip に入っているので、そのまま音が出る。

## 2. SDR 側を待ち受けにする

デコーダは **TCP クライアント**として繋ぎにいくので、SDR 側をサーバにしておく。

### SDR#（Windows）

zip の `sdrsharp-plugin\SDRSharp.IqTcpServer.dll` を SDR# の `Plugins\` フォルダへコピーし、
**`SDRSharp.dotnet9.exe`（.NET 9 ホスト）**で SDR# を起動すると、右ペインに
**"IQ TCP Server (cf32)"** が出る（詳細は `sdrsharp-plugin\README.md`）。放送のチャネルへ
同調して再生（▶）→ **Start server**。パネルに表示される送出レートを `--fs` に渡す。

プラグインは**選択中の VFO を 0 Hz へ落として間引いてから**流すので、周波数を変えたいときは
SDR# のスペクトラムをクリックするだけでよい（デコーダは再起動不要）。

### SDR++

IQ Exporter（Network Sink）を **TCP / Int16** で待ち受けにして、そのサンプルレートを渡す。

### rtl_tcp

`rtl_tcp -a 0.0.0.0` で起動しておく。周波数と利得はデコーダ側から設定する。

## 3. デコーダを起動する

```sh
# SDR#（cf32、プラグインのレートが 64000 の場合）
./std-t86             live tcp://127.0.0.1:5555 --fs 64000 --fmt cf32
./std-t115-qpsknarrow live -fs 64000 -fmt cf32 tcp://127.0.0.1:5555

# SDR++（Int16）
./std-t86             live tcp://127.0.0.1:5555 --fs 192000 --fmt cs16
./std-t115-qpsknarrow live -fs 192000 -fmt s16 tcp://127.0.0.1:5555

# rtl_tcp（周波数はデコーダから指定する）
./std-t86             live rtltcp://127.0.0.1:1234 --fs 1024000 --freq 60000000
./std-t115-qpsknarrow live -fs 1024000 -freq 60000000 rtltcp://127.0.0.1:1234
```

Windows は `std-t86.exe` / `std-t115-qpsknarrow.exe`（コマンドプロンプトか PowerShell から。
エクスプローラでダブルクリックしても引数を渡せない）。

`std-t115-qpsknarrow` は**オプションを入力より先に**書くこと（Go の flag は最初の非オプション引数で
解析を止めるため）。

録音ファイル（`.wav` は I=L / Q=R、`.cu8`、`.cf32`）も同じように再生できる。

```sh
./std-t86             live recording.wav --offset 0
./std-t115-qpsknarrow live recording.wav
```

## 4. ブラウザで聴く

起動したら **http://127.0.0.1:8000/** を開く。受信できていれば同期がかかり、放送が始まると
通報として検出される。

音声ペインの **「再生開始」ボタン**を押すと鳴りはじめる（ブラウザの自動再生制限があるので、
最初の 1 回はクリックが要る）。通報が終わると WAV としてダウンロードもできる。

両方を同時に動かすときは、どちらかの待ち受けをずらすこと（`std-t86 live … --port 8001` /
`std-t115-qpsknarrow live -addr 127.0.0.1:8001 …`）。

## よく使うオプション

| STD-T86 | STD-T115 | 意味 |
|---|---|---|
| `--fs` | `-fs` | サンプルレート [Hz]（TCP では必須） |
| `--fmt` | `-fmt` | 標本形式。`cf32` = SDR# プラグイン、SDR++ の Int16 は T86 が `cs16`・T115 は `s16`。ほかに `cu8` |
| `--offset` | `-offset` | チャネルのベースバンドオフセット [kHz]。**同調済みなら 0**（既定） |
| `--freq` | `-freq` | rtl_tcp の同調周波数 [Hz] |
| `--port` / `--host` | `-addr` | モニタの待ち受け（既定 `127.0.0.1:8000`） |
| `--log-dir` | `-log-dir` | 通報音声・I/Q の保存先（既定 `logs/`） |
| `--full-speed` | `-full-speed` | 録音ファイルを実時間ではなく全速で処理する |

## 音が出ないとき

1. **`build/` フォルダを実行ファイルの隣に置いたままか**（手順 1）。STD-T115 は
   同梱の `build_amrwbplus.ps1` / `build_amrwbplus.sh` を 1 回走らせて
   `build/amrwbplus/` を作る。未配置だと画面のログに警告が出る。
2. **「再生開始」を押したか**。ブラウザは操作なしに音を鳴らせない。
3. **同期しているか**。画面上部の CRC 一致率が 0 のままなら、`--fs` が SDR 側の送出レートと
   合っていないか、`--fmt` の形式が違うか、チャネルに同調できていない。
4. **放送していない**。同報無線は常時送信ではないので、放送が無い時間帯は制御信号すら
   出ないことがある（STD-T115 は待機中は電波が出ない）。

## ソースからビルドする

配布していない OS / CPU で動かしたいときや、自分で改造するとき。Go と C コンパイラが要る。

```sh
# デコーダ本体
go build -o std-t86             ./cmd/std-t86
go build -o std-t115-qpsknarrow ./cmd/std-t115/qpsknarrow

# 音声デコーダ（使うほうだけでよい）
bash scripts/std-t86/build_g7221.sh        # STD-T86 用（G.722.1）
bash scripts/std-t115/build_amrwbplus.sh   # STD-T115 用（AMR-WB+）
```

Windows は `pwsh scripts/std-t86/build_g7221.ps1` /
`pwsh scripts/std-t115/build_amrwbplus.ps1`。MSVC の `cl` は非対応（gcc 系が要る）だが、
**どちらのスクリプトも gcc が無ければ自分で MinGW-w64 を用意する**ので、
用意しておくのは Go だけでよい。

音声デコーダのビルドは必須ではないが、無いと**音が出ない**（制御チャネルの復号と画面表示は
動く）。AMR-WB+ の参照ソースは再頒布できないため、スクリプトが取得・パッチ・ビルドまでを行う。

## 本プログラムの作成方法

全てのコードは Claude Code により生成されている。また、インターネット上の公開情報と実電波のみを元に作成している。
そのため、誤りが多数あると思われる。

## 参考文献
- 市町村デジタル同報通信システムの規格等に関する調査検討報告書  
https://warp.ndl.go.jp/web/20090113093911/http://www.soumu.go.jp/s-news/2003/pdf/030425_2_03.pdf
- DIGITAL SIMULTANEOUS COMMUNICATION SYSTEMS FOR MUNICIPALITIES GOVERNMENT TYPE2  
https://www.arib.or.jp/english/html/overview/doc/5-STD-T115v2_7-E1.pdf
