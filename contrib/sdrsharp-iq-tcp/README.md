# SDR# IQ TCP Server プラグイン

SDR# の I/Q を **TCP サーバ**として配信し、arib の各デコーダ（`std-t86` / `std-t115-qpsknarrow` /
`std-t61` …）から **`sdrsharp://host:port`** で受けるためのプラグイン。デコーダは TCP クライアント
として接続しにくる側なので、SDR# が待ち受けサーバになる。

**サンプルレート・標本形式・周波数は接続の中で伝わる**ので、デコーダに `-fs` / `-fmt` を
渡さなくてよい（下の「送出の形式」）。

既定では **SDR# で選択中のチャネル（VFO）をプラグイン側で 0 Hz へ落として（DDC）
間引いてから**流す。デコーダはチャネルオフセット固定運用（実行中に作り直さない）なので、
この形にすると **CLI の局の指定（`-channels`）を触らずに、SDR# のスペクトラムをクリックするだけで
受信周波数を変えられる**（ベースバンド録音を SDR# で再生しながら、いろいろな周波数の
受信を試す用途）。デコーダは `-channels` を付けずに起動する（中心 = 局、オフセット 0 の 1 局）。

I/Q は **cf32（float32 の I,Q インターリーブ）**。SDR# の `Complex` は
`{float Real; float Imag;}` の 8 バイトで、メモリ配置がそのまま cf32 なので変換ゼロ・
バイトコピーのみで送出する。

## 送出の形式（`sdrsharp://`）

1 本の TCP 接続に**枠**を並べて流す。受け手の実装は `internal/iqsrc/sdrsharp.go`、
書式の正本は設計書 `docs/architecture-plan.md` の 2.4（**3 か所を揃えること**）。

```
枠 = 種類 1 バイト + 長さ 4 バイト（uint32 little-endian、中身のバイト数）+ 中身

種類 0x01  I/Q の塊。中身は cf32（float32 little-endian の I,Q 交互）。長さは 8 の倍数
種類 0x02  同調の通知。中身は UTF-8 の JSON {"center":<Hz>,"vfo":<Hz>,"rate":<Hz>}
それ以外    受け手は読み飛ばす（種類を足しても古い受け手が壊れない）
```

- **接続直後の最初の枠は必ず同調の通知**。SDR# の再生前（レート未確定）は何も送らない。
  受け手は最初の枠が通知でなければ「この形式ではない」として接続を断る（10 秒待っても
  来なければ「再生を始めてから Start server」と出して諦める）。
- 通知は center / vfo / rate のどれかが変わるたびに送り直す。**通知より後の I/Q はその
  通知に従う**。レートの変化は次の I/Q より前に通知する。VFO の変化はパネルのタイマ
  （200ms）の粒度で遅れて届く。
- `center` は**送出する I/Q の 0 Hz に当たる周波数**、`vfo` は SDR# の VFO の周波数。
  VFO 追従（既定）では VFO を 0 Hz へ落とすので両者は等しく、追従を外すと
  `center` = デバイスの中心周波数になる。目的のチャネルのずれは常に `vfo − center`。
- 長さの上限は I/Q 16 MiB・通知 4 KiB（受け手はそれを超えたら壊れた接続として切る）。
- 送出キューがあふれたら**最古の I/Q の枠**を捨てる。**通知の枠は捨てない**。
- 受け手からは何も送らない（周波数や利得を受け手から変える口は未実装）。

**生の cf32 だけを流す以前の形式はやめた**（`tcp://` で受けるには `-fs` と `-fmt` を
手で合わせる必要があり、VFO を動かしても受け手に周波数が伝わらなかった）。
`tcp://` は SDR++ の Network Sink のように形式を決められない相手のために残してある。

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
2. **`SDRSharp.dotnet9.exe`** を起動すると右ペインに **"IQ TCP Server (sdrsharp://)"** パネルが出る。

## 使い方（VFO 追従 = 既定）

1. SDR# でデバイスかベースバンド録音（File Player）を選んで再生（▶）。
2. パネルの **Follow VFO (DDC → 0 Hz)** と **Decimate stream** はどちらも既定 ON のまま、
   **Port**（既定 5555）を決めて **Start server**。
3. パネルに出ているコマンドをそのまま実行（レートも形式も渡さない）:

   ```sh
   std-t86 live -input sdrsharp://127.0.0.1:5555
   std-t115-qpsknarrow live -input sdrsharp://127.0.0.1:5555
   arib probe -input sdrsharp://127.0.0.1:5555        # 規格が分からないとき（全規格で同時に試す）
   # 別ホストの SDR# なら 127.0.0.1 をそのマシンの IP に
   ```
4. **あとは SDR# のスペクトラムで目的のチャネルをクリックするだけ**。VFO オフセットは
   200ms 毎にプラグインへ反映され、常に 0 Hz へ落とされて流れる。デコーダ側は
   接続の中の周波数の通知を読み、`-input` の中心を省いた 1 局の受信なら**局を VFO の周波数へ付け替える**
   （画面の局の周波数・録音の名前もそれに従う。格子の半分以内の微調整なら局は残して捕捉だけやり直す）。
   別の自治体のチャネルへ移っても自動で追従する（`-municipal-code` を付けると seed は
   固定されるので、周波数を渡り歩くなら付けない）。

パネルの表示:

| 表示 | 意味 |
|---|---|
| `Rate: 1,024,000 Hz / 16 → 64,000 Hz` | デバイスレート / 間引き比 → 送出レート（= 通知の `rate`） |
| `Measured: 64,003 Hz` | 実測送出レート。公称と大きく違うなら SDR# 側（デバイスのレート設定）を疑う |
| `VFO offset: -83.000 kHz → 0 Hz` | いま 0 Hz へ落としているオフセット（`Frequency − CenterFrequency`） |
| `Level: -32.1 dBFS` | 送出信号の RMS。チャネルを外していると一気に下がるので当たりの確認に使える |

### 間引きと帯域

- 間引きは SDR# 内蔵の `ComplexDecimator` に任せ、**送出レートがパネルの
  `Min rate (kHz)`（既定 40）を下回らない最大比**を選ぶ（1.024 MS/s → 1/16 = 64 kHz）。
  T86 のチャネルは 11.25 kbaud・RRC β=0.5 の ±約 8.4 kHz なので 40 kHz で余裕がある
  （T86 の I/Q 録音が同じ 40 kHz 級の DDC 保存 → 再デコードで元復号と一致することを
  実波で確認済み）。T115 QPSK ナロー（7.5 kHz 間隔）・T61（6.25 kHz 間隔）も 40 kHz で足りる。
  間引き比は 2 のべき乗なので、下限を少し変えても送出レートは段階的にしか変わらない。
- 帯域も軽くなる: cf32 8 byte/sample なので 1.024 MS/s の素通しは ≈8 MB/s、64 kHz なら
  ≈0.5 MB/s。デコーダ側の前段デシメーションも短くなる。
- **Decimate stream** を OFF にするとデバイスレートのまま DDC だけ行う（送出レート = デバイスレート）。
- **Follow VFO** を OFF にすると生 I/Q 素通し。この場合はチャネルを SDR# の
  中心周波数側で合わせ、デコーダに `-channels <局 MHz> -input sdrsharp://…@<中心 MHz>` を渡す運用になる（通知の `center` は
  デバイスの中心周波数、`vfo` は VFO なので、受け手が `vfo − center` を使えば手で渡さずに済む。
  デコーダ側がこれを読むのは今後）。
- モード切替（Follow VFO / Decimate / Min rate）は送出レートを変えるため、サーバ稼働中は
  変更できない（ポートと同様）。レートは通知で受け手へ伝わるが、今のデコーダは起動時の
  レートで復調器を作るので、Stop → 切替 → Start のあとデコーダも起動し直す。

## 構成

| ファイル | 役割 |
|---|---|
| `build.ps1` | SDK 自動 DL → `refs/` 展開 → ビルド |
| `IqTcpServerPlugin.cs` | `ISharpPlugin` + `IIQProcessor`。RawIQ フックを登録し、DDC を通してサーバへ渡す |
| `IqDdc.cs` | VFO 追従 DDC（NCO で 0 Hz へ → `ComplexDecimator` で間引き）+ レベル/標本数の計測 |
| `IqTcpServer.cs` | マルチクライアント TCP サーバ。枠付きの形式で送る。クライアント毎に有界キュー + 送信スレッド（あふれたら最古の I/Q を破棄。通知は捨てない） |
| `IqTcpServerPanel.cs` | 最小 WinForms UI（ポート・モード・出力レートの下限・状態・コマンド表示） |
| `refs/` | SDK から展開した参照 DLL（gitignore 済み） |

## 注意

- DDC の NCO の向きは `exp(-j2πf n/fs)`（周波数 f を 0 へ移す）なので、オフセットの符号は
  CLI の局のオフセット（`-channels` の局 − `-input` の中心）と同義（VFO が中心より上なら正）。SDR# の **Swap I&Q** を ON にすると符号が反転するので
  併用しないこと。ハードウェア IF 用の `IF Offset` は考慮していない（既定 0 のまま使う）。
- SDR# の復調チェーンには手を出さない（フックは RawIQ のみ・バッファは書き換えずコピー）。
  SDR# 側のフィルタ帯域・復調モード・音声は何に設定していても配信内容に影響しない。
- 送出キューは自前の有界キュー（`LinkedList` + `Monitor`）。処理が追いつかない時は
  最古の I/Q の枠から捨てる（実時間ソースは取りこぼし前提）。通知の枠を捨てないために
  `System.Threading.Channels` の `DropOldest` はやめた。
- 旧 .NET Framework 版 SDR# 用の TcpServer プラグイン（`.NETFramework v4.6`）は
  .NET 9 版ホストに load できない。これはその置き換え。
- SDR# を `SDRSharp.dotnet8.exe`（.NET 8 ホスト）で使いたい場合は、csproj の
  `<TargetFramework>` を `net8.0-windows` に変えて `-p:RefsDir` に .NET 8 版の
  参照 DLL を渡す（net9 ビルドは .NET 8 ホストに load できない）。
