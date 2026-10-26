# 法的注意

このソフトウェアは受信だけを行う。送信の機能は無い。

条文は [e-Gov 法令検索の電波法](https://laws.e-gov.go.jp/law/325AC0000000131) で確かめること。以下は法律相談ではない。

## 電波法

- 第 59 条: 特定の相手方に対して行われる無線通信を傍受して、その存在や内容を漏らし、又は窃用してはならない。
- 第 109 条: 無線通信の秘密を漏らし又は窃用した者は、1 年以下の拘禁刑又は 50 万円以下の罰金。無線通信の業務に従事する者がその業務に関し知り得た秘密であれば、2 年以下の拘禁刑又は 100 万円以下の罰金。
- 第 109 条の 2: 暗号通信の秘密を漏らし又は窃用する目的でその内容を復元した者は、1 年以下の拘禁刑又は 50 万円以下の罰金。

## してはいけないこと

- 受信した音声・内容・位置・識別番号を、SNS・配信・掲示板・ブログなどに載せる
- Web モニタを LAN やインターネットへ開く（既定の待ち受けは `127.0.0.1`）
- 録音や記録（`logs/`）を人に渡す
- 通信の内容を、取材・営業・人や車両の追跡などに使う

受信したものには、人の声・位置・識別番号など個人に結びつく情報が含まれる。記録は手元に置き、要らなくなったら消すこと。

## 規格書と特許

ARIB 標準規格の著作権は電波産業会（ARIB）にある。このリポジトリは規格書の本文を含まない。
音声符号器には特許が関わるものがあり、配布物には入れていない。自分でビルドして使ってよいかは、利用者が確かめること。
第三者のものの条件は `THIRD_PARTY_NOTICES.md` にある。

## 免責

関係法令に従って使うことを前提に公開している。個々の利用が法令に適合するかの判断と、その結果の責任は利用者にある。
作者は、利用によって生じた損害や紛争について責任を負わない。

## English

This software only receives; it cannot transmit. Under Article 59 of Japan's Radio Act, you must not disclose or make use of
intercepted communications addressed to a specific party (penalties under Articles 109 and 109-2). Do not publish or share what
you receive, and do not expose the web monitor to a network. You are responsible for using this software lawfully.
This is not legal advice.
