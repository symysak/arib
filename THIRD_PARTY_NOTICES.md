# サードパーティの表示 / Third-party notices

このリポジトリ自身のコードは (c) 2026 symysak。
ここに挙げるのはそれ以外のもので、**それぞれの条件に従う**。

## 実行ファイルに組み込まれているもの

### Go の標準ライブラリとランタイム

すべての実行ファイルに静的に組み込まれている。BSD-3-Clause。

```
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### github.com/coder/websocket

Web モニタの WebSocket。ISC License。

```
Copyright (c) 2025 Coder

Permission to use, copy, modify, and distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```

## リポジトリと配布 zip に入っているもの

### ITU-T G.722.1 参照実装

`third_party/T-REC-G.722.1-200505-I!!SOFT-ZST-E/` は ITU-T が配布する G.722.1 の
C ソースとテストベクタで、加工せずに置いてある。`std-t86` の zip の `build/g7221/` は
これをパッチしてビルドしたもの。

- 著作権表示: (c) 2005 Polycom, Inc. All rights reserved.
- 条件は ITU-T の配布条件に従う。由来は zip の `build/g7221/ITU-G.722.1-Readme.txt`
  （リポジトリでは `Software/Fixed-200505-Rel.2.1/Readme.txt`）。

## 同梱していないもの

### AMR-WB+ 参照実装（3GPP TS 26.304）

`std-t115-qpsknarrow` の音声に使う。3GPP の配布物は書面の許可なく再頒布できないので
同梱しない。`build_amrwbplus.sh` / `build_amrwbplus.ps1` が 3GPP から取得してビルドする。
取得したものは 3GPP の条件に従う。

### mbelib-neo（symysak/mbelib-neo。soltia48/mbelib-neo のフォーク）

`std-t79` の音声（AMBE+ 4000/2400）に使う。GPL-2.0-or-later。同梱しない（実行ファイルにも
組み込まない。子プロセスとして呼ぶ）。`build_mbelib_neo.sh`（Windows は `build_mbelib_neo.ps1`）が GitHub から取得してビルドする。取得したものは
GPL の条件に従う。音声の表は近似（mbelib-neo 自身がそう明記している）。

### SDR#

`sdrsharp-plugin/`（Windows の zip）はこのリポジトリの `contrib/sdrsharp-iq-tcp` を
ビルドしたもの（(c) 2026 symysak）。SDR# 本体のファイルは参照するだけで同梱しない。

## データ

### 市区町村コード表（`internal/citycodes/`）

出典: e-Stat 政府統計の総合窓口「統計に用いる標準地域コード」
（https://www.e-stat.go.jp/municipalities/cities/areacode ）および「廃置分合等情報」
（https://www.e-stat.go.jp/municipalities/cities/absorption-separation-of-municipalities ）
を加工して作成。
