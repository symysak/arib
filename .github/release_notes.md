## 使い方

1. お使いの OS / CPU 向けの `.zip` を展開する
2. 展開したフォルダの `stdt86`（Windows は `stdt86.exe`）を実行する

```
stdt86 live <I/Q ファイル または tcp://host:port> --offset <kHz>
```

Web モニタは http://127.0.0.1:8000/ で開く。オプションは `readme.md` を参照。

**`build/` フォルダは実行ファイルと同じ場所に置いたままにすること。** 音声デコードに
使う ITU-T G.722.1 のバイナリが入っており、実行ファイルからの相対位置で探す。
消しても受信・制御チャネルの復号は動くが、音声だけが出なくなる。

### macOS

ダウンロードした zip には Gatekeeper の検疫属性が付くので、初回だけ外す:

```
xattr -dr com.apple.quarantine <展開したフォルダ>
```

### Windows: SDR# プラグイン

Windows 版の zip には `sdrsharp-plugin/SDRSharp.IqTcpServer.dll` を同梱している。
SDR# の `Plugins\` フォルダへコピーし、**`SDRSharp.dotnet9.exe`（.NET 9 ホスト）**で
SDR# を起動すると、選択中の VFO を 0 Hz へ落として間引いた I/Q を TCP で配信できる。
デコーダ側は常に `--offset 0` で受ける:

```
stdt86.exe live tcp://127.0.0.1:5555 --offset 0 --fs 64000 --fmt cf32
```

`--fs` はプラグインのパネルに出る送出レート（1.024MS/s 入力なら 64000）。詳細は
`sdrsharp-plugin/README.md`。SDR# 本体のアセンブリは同梱していない（参照のみ）。

### 同梱物

- `stdt86` — 本体。純 Go（cgo なし）で、Web モニタの静的ファイルと
  市区町村コード表はバイナリに埋め込んである
- `build/g7221/*` — ITU-T G.722.1 (05/2005) Release 2.1 参考実装をビルドしたもの。
  著作権表示は `build/g7221/ITU-G.722.1-Readme.txt`（(c) 2005 Polycom, Inc.）
- `sdrsharp-plugin/*` — Windows 版のみ
- `readme.md`
