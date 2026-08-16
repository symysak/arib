# SDR# IQ TCP Server プラグイン

SDR# の I/Q を **TCP サーバ**として配信し、std-t86 デコーダから `tcp://` ソースで
受けるためのプラグイン。デコーダは TCP クライアントとして接続しにくる側なので、
SDR# が待ち受けサーバになる。

既定では **SDR# で選択中のチャネル（VFO）をプラグイン側で 0 Hz へ落として（DDC）
間引いてから**流す。デコーダはチャネルオフセット固定運用（実行中に作り直さない）なので、
この形にすると **CLI の `--offset` を触らずに、SDR# のスペクトラムをクリックするだけで
受信周波数を変えられる**（ベースバンド録音を SDR# で再生しながら、いろいろな周波数の
受信を試す用途）。デコーダは常に `--offset 0` で起動する。

配信フォーマットは **cf32（float32 の I,Q インターリーブ）**。SDR# の `Complex` は
`{float Real; float Imag;}` の 8 バイトで、メモリ配置がそのまま cf32 なので変換ゼロ・
バイトコピーのみで送出する。

対象は Airspy の **.NET 版 SDR#**。公式プラグイン SDK に合わせて `net9.0-windows` で
ビルドするので、**`SDRSharp.dotnet9.exe`（.NET 9 ホスト）で起動**すること。実 API
（`ISharpPlugin.Gui`、`ISharpControl.RegisterStreamHook(object, ProcessorType)`、
`IIQProcessor.Process(Complex*, int)`、`ComplexDecimator`）に対しビルド検証済み。

## ビルド（ワンステップ）

`.NET 9 SDK` が必要（未導入なら `winget install Microsoft.DotNet.SDK.9`）。あとは:

```powershell
pwsh ./build.ps1
```

`build.ps1` が自動で:
1. SDR# プラグイン SDK zip（`https://airspy.com/?ddownload=5944`）をダウンロード（`.sdk-cache/` にキャッシュ）
2. 中の `SDRSharp.Common.dll` / `SDRSharp.Radio.dll` を `refs/` へ展開
3. `dotnet build -c Release` でプラグインをビルド

出力: `bin/Release/net9.0-windows/SDRSharp.IqTcpServer.dll`

オプション:
```powershell
pwsh ./build.ps1 -Force                 # SDK を再ダウンロードして refs を更新
pwsh ./build.ps1 -Configuration Debug
# 手元に SDK の lib フォルダがあるなら DL を省略:
dotnet build -c Release -p:RefsDir="C:\path\to\sdrplugins\lib"
```

## インストール

この SDR# は `Plugins\` フォルダを自動スキャンする（`SDRSharp.config` の
`core.pluginsDirectory=Plugins`）。**`Plugins.xml` への登録は不要。**

1. `SDRSharp.IqTcpServer.dll` を SDR# の `Plugins\` フォルダへコピー。
2. **`SDRSharp.dotnet9.exe`** を起動すると右ペインに **"IQ TCP Server (cf32)"** パネルが出る。

## 使い方（VFO 追従 = 既定）

1. SDR# でデバイスかベースバンド録音（File Player）を選んで再生（▶）。
2. パネルの **Follow VFO (DDC → 0 Hz)** と **Decimate stream** はどちらも既定 ON のまま、
   **Port**（既定 5555）を決めて **Start server**。
3. パネルに出ているコマンドをそのまま実行（`--fs` はパネルの Rate 表示と一致する値）:

   ```sh
   stdt86 live tcp://127.0.0.1:5555 --offset 0 --fs 64000 --fmt cf32
   # 別ホストの SDR# なら 127.0.0.1 をそのマシンの IP に
   ```
4. **あとは SDR# のスペクトラムで目的のチャネルをクリックするだけ**。VFO オフセットは
   200ms 毎にプラグインへ反映され、常に 0 Hz へ落とされて流れる。デコーダ側は制御
   チャネルが 1 秒途絶えると CFO 再捕捉とスクランブル値の再探索に入るので、別の
   自治体のチャネルへ移っても自動で追従する（`--municipal-code` を付けると seed は
   固定されるので、周波数を渡り歩くなら付けない）。

パネルの表示:

| 表示 | 意味 |
|---|---|
| `Rate: 1,024,000 Hz / 16 → 64,000 Hz` | デバイスレート / 間引き比 → 送出レート（= `--fs`） |
| `Measured: 64,003 Hz` | 実測送出レート。公称と大きく違うなら `--fs` を実測値に合わせる |
| `VFO offset: -83.000 kHz → 0 Hz` | いま 0 Hz へ落としているオフセット（`Frequency − CenterFrequency`） |
| `Level: -32.1 dBFS` | 送出信号の RMS。チャネルを外していると一気に下がるので当たりの確認に使える |

### 間引きと帯域

- 間引きは SDR# 内蔵の `ComplexDecimator` に任せ、**送出レートが 40 kHz を下回らない
  最大比**を選ぶ（1.024 MS/s → 1/16 = 64 kHz）。T86 のチャネルは 11.25 kbaud・RRC β=0.5 の
  ±約 8.4 kHz なので余裕がある（`server/iq_recorder.py` が同じ 40 kHz 級の DDC 保存 →
  再デコードで元復号と一致することを実波で確認済み）。
- 帯域も軽くなる: cf32 8 byte/sample なので 1.024 MS/s の素通しは ≈8 MB/s、64 kHz なら
  ≈0.5 MB/s。デコーダ側の前段デシメーションも短くなる。
- **Decimate stream** を OFF にするとデバイスレートのまま DDC だけ行う（`--fs` はデバイスレート）。
- **Follow VFO** を OFF にすると旧挙動（生 I/Q 素通し）。この場合はチャネルを SDR# の
  中心周波数側で合わせ、デコーダに `--offset <kHz>` を渡す運用になる。
- モード切替は送出レートを変えるため、サーバ稼働中は変更できない（ポートと同様）。
  Stop → 切替 → Start で、デコーダも新しい `--fs` で起動し直す。

## 構成

| ファイル | 役割 |
|---|---|
| `build.ps1` | SDK 自動 DL → `refs/` 展開 → ビルド |
| `IqTcpServerPlugin.cs` | `ISharpPlugin` + `IIQProcessor`。RawIQ フックを登録し、DDC を通してサーバへ渡す |
| `IqDdc.cs` | VFO 追従 DDC（NCO で 0 Hz へ → `ComplexDecimator` で間引き）+ レベル/標本数の計測 |
| `IqTcpServer.cs` | マルチクライアント TCP サーバ。クライアント毎に有界キュー + 送信スレッド（あふれたら最古破棄） |
| `IqTcpServerPanel.cs` | 最小 WinForms UI（ポート・モード・状態・コマンド表示） |
| `refs/` | SDK から展開した参照 DLL（gitignore 済み） |

## 注意

- DDC の NCO の向きは `server/iq_recorder.py` / `dsp/stream_frontend.py` の `_NCO` と同じ
  （`exp(-j2πf n/fs)` = 周波数 f を 0 へ移す）ので、オフセットの符号は CLI の `--offset` と
  同義（VFO が中心より上なら正）。SDR# の **Swap I&Q** を ON にすると符号が反転するので
  併用しないこと。ハードウェア IF 用の `IF Offset` は考慮していない（既定 0 のまま使う）。
- SDR# の復調チェーンには手を出さない（フックは RawIQ のみ・バッファは書き換えずコピー）。
  SDR# 側のフィルタ帯域・復調モード・音声は何に設定していても配信内容に影響しない。
- `System.Threading.Channels`（`BoundedChannelFullMode.DropOldest`）を使う。処理が
  追いつかない時は最古ブロックから捨てる（実時間ソースは取りこぼし前提）。
- 旧 .NET Framework 版 SDR# 用の TcpServer プラグイン（`.NETFramework v4.6`）は
  .NET 9 版ホストに load できない。これはその置き換え。
- SDR# を `SDRSharp.dotnet8.exe`（.NET 8 ホスト）で使いたい場合は、csproj の
  `<TargetFramework>` を `net8.0-windows` に変えて `-p:RefsDir` に .NET 8 版の
  参照 DLL を渡す（net9 ビルドは .NET 8 ホストに load できない）。
