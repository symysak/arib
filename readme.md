# STD-T86 受信デコーダ

ARIB STD-T86（市町村デジタル同報通信システム、60MHz帯 16QAM TDMA-TDD）の OSS 受信デコーダ。
SDR# で同調した生 I/Q を受け取り、復調 → 制御チャネル復号 → 通報検出 → S-Codec 音声デコード
までを行って、ブラウザのライブモニタに表示する。

## 必要なもの

- Python 3.11 以上と [uv](https://docs.astral.sh/uv/)
- Windows の SDR#（.NET 9 版）と本リポジトリ同梱のプラグイン `contrib/sdrsharp-iq-tcp/`
- 60MHz 帯を受信できる SDR（R820T2 系なら direct sampling モッド不要）
- C コンパイラ（音声デコード用の G.722.1 バイナリをビルドするため）

## セットアップ

```sh
uv sync --all-groups           # 依存導入
bash scripts/build_g7221.sh    # 同梱 ITU G.722.1 をパッチ・ビルド → build/g7221/
pwsh scripts/build_g7221.ps1   # Windows 版（MinGW-w64 の gcc/clang が必要。MSVC cl は非対応）
```

G.722.1 バイナリが無くても制御チャネルの復号とモニタ表示は動くが、音声デコードだけ失敗する。
`codec/g7221.py` は `build/g7221/` を見にいく（`STDT86_G7221_DIR` で場所を上書き可）。

## SDR# から受信する

1. **プラグインをビルドして入れる**（詳細は `contrib/sdrsharp-iq-tcp/README.md`）。

   ```powershell
   cd contrib/sdrsharp-iq-tcp
   pwsh ./build.ps1
   # bin/Release/net9.0-windows/SDRSharp.IqTcpServer.dll を SDR# の Plugins\ へコピー
   ```

   `SDRSharp.dotnet9.exe` を起動すると右ペインに **"IQ TCP Server (cf32)"** パネルが出る。

2. **SDR# で対象チャネルへ同調して再生（▶）**し、パネルの **Port**（既定 5555）で
   **Start server**。パネルに `--fs` に渡すサンプルレートが表示される。

3. **デコーダを接続する。**

   ```sh
   uv run stdt86 live tcp://127.0.0.1:5555 --fs 1024000 --fmt cf32
   # → http://127.0.0.1:8000/ をブラウザで開く
   ```

   別ホストの SDR# なら `127.0.0.1` をそのマシンの IP に変える。生 I/Q は cf32 で
   8 byte/sample（1.024 MS/s なら ≈8 MB/s）流れるので、無線 LAN 越しは注意。

### オプション

| オプション | 意味 |
|---|---|
| `--fs` | サンプルレート [Hz]（ネットワークソースでは必須） |
| `--fmt` | 形式。SDR# プラグインは `cf32` |
| `--offset` | チャネルのベースバンドオフセット [kHz]。**SDR# で同調済みなら省略（=0）** |
| `--municipal-code` / `--seed` | スクランブル値。省略すると自動判定する |
| `--log-dir` | 通報音声・I/Q の保存先（既定 `logs/`） |
| `--host` / `--port` | モニタの待ち受け（既定 `127.0.0.1:8000`） |

**チャネルオフセットは固定**で、起動後の再探索はしない（同調は SDR# 側の仕事）。
受信中の搬送波ドリフトは前段の CFO 追尾が吸収する。市区町村コードを省略した場合は、
シンドローム重みで候補を絞り、ビタビ復号後の CRC16・既知種別・製造者スコアで確定する
（確信が持てるまでスライディング窓で再試行）。

## モニタでできること

- 制御チャネル状態（市区町村・拡声中・使用スロット・製造者）
- 通報タイムライン（0x22 通報開始 〜 0x30 強制切断）と報知対象（一斉／群・個別）
- 信号品質（ウォーターフォール、コンスタレーション、CFO、EVM、CRC 一致率）とデコードログ
- 通報音声の S-Codec デコードをブラウザでストリーミング再生（🔊）、WAV ダウンロード
- 通報検知時の I/Q 録音（プリロール 5s つき、低レート DDC で 2ch int16 WAV）。保存した
  ファイルは `uv run stdt86 live <file> --offset 0` でそのまま再デコードできる

音声は伝送路 FEC の受信波同定版（`s_codec.transmission_decode_ota`）で復号する。CRC7 が
落ちたフレームは PLC（直近正常フレーム反復・長区間ミュート）で補間してパイプラインは止めず、
CRC7 統計を報告し続ける。
