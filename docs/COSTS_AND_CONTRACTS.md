# 維持費・契約・更新が必要なもの

運用責任者（と後任）が、**何にいくらかかり、何を誰がいつ更新するか**を把握するための一覧。金額・契約者・期限は、コードやリポジトリからは分からない項目が多い。「（要記入）」は運用責任者が把握して書き込む（書いたら最終更新日を更新）。**パスワード・トークン・APIキーの値は書かない**（場所と更新手順だけ。[RUNBOOK](RUNBOOK.md)）。

最終更新: 2026-11-28

## 1. 毎月・毎年かかるもの

| 項目 | 内容 | 費用 | 支払い・契約者 | 更新・期限 |
|---|---|---|---|---|
| VPS | さくらのVPS v5・石狩第1・仮想4コア／メモリ約4GB／SSD 200GB・Ubuntu 24.04。2026-11-26に契約（無料お試し期間中として開始） | （要記入：プランの月額・年額） | （要記入：契約者・支払い方法） | **無料お試し期間の終了前に本契約へ移行する**（要記入：期限）。更新を忘れるとサイト・ポータル・ぬるが止まる |
| ドメイン | `nu-digital.jp`（公式サイト `nu-digital.jp`・ポータル `app.nu-digital.jp`） | （要記入：年額） | （要記入：レジストラ・契約者） | （要記入：更新期限）。**期限切れでサイトが止まる**。更新の担当と時期を決めておく |
| DNS | Cloudflare（DNSのみ。プロキシなし） | （要記入：プランと費用。DNSだけなら無料枠の見込みだが要確認） | （要記入：Cloudflareのアカウント所有者） | なし（アカウントの引き継ぎが必要。RUNBOOK §12） |
| ぬるのClaude API | 実行時のAPI利用。**Claude Teamプラン付属のAPIクレジット 月$60の範囲だけ**。Console側の使用上限$60・支払い方法は登録しない・auto-reloadなし（追加課金なし） | 上限 $60/月（実際の使用は、会話・Batch・Web検索で月数ドル〜の見込み。実績を§4で確認） | Teamプランの契約者（要記入） | **APIキーの有効期限**（90日などで発行）。期限の14日前から、ぬるがtimesで催促する（`settings.apikey.expires`）。更新したら新しい日付を設定（[NUL_LAUNCH §6.5](NUL_LAUNCH.md)） |
| 開発用のClaude（Claude Code） | 開発作業はClaude Teamの内包使用量だけを使う。追加課金・API・Fableは使わない（[CLAUDE_TEAM](CLAUDE_TEAM.md)） | Teamプランの席の費用（要記入） | （要記入） | Teamの管理者が、追加使用量・自動チャージを無効にしてあることを確認 |
| GitHub | Organization `nu-digital`・リポジトリ`nu-digital/nu_app`・Actions（CI・デプロイ）・GHCR（コンテナイメージの置き場） | （要記入：Organizationのプラン。Actionsの実行時間・GHCRの容量が無料枠に収まっているか） | （要記入：Organizationのオーナー） | 承認者（環境`production`の必須レビュアー）の更新（卒業・交代のとき。RUNBOOK §6・§12） |

## 2. 無料だが、期限・更新・失効の管理が必要なもの

| 項目 | 内容 | 更新・失効 |
|---|---|---|
| TLS証明書 | Let's Encrypt（`nu-digital.jp`・`app.nu-digital.jp`）。取得 2026-11-26・期限 2027-02-23 | **自動更新**（certbot.timer）。更新できないときは、14日前からDiscordに通知（`nu-cert-check`）。メールは登録していない（RUNBOOK §4） |
| Discord Bot（ぬる） | Discord Developer Portalのアプリ・Botのtoken | tokenの漏えいが疑われたらReset Token→`.env`更新。アプリの所有者を後任へ移す（Teamにする等。RUNBOOK §12） |
| Discord Bot（nu_appの通知）・Webhook | 運営向けの通知（バックアップ・デプロイ・証明書）のWebhook URL | 作り直したら`/etc/nu_app/backup.env`を更新（RUNBOOK §7） |
| GHCR用のPAT（classic） | VPSがイメージをpullするための`read:packages`のトークン（作成者のGitHubアカウントに紐づく） | **作成者が卒業・退会すると失効**する。後任が作り直してVPSで`docker login`し直す（RUNBOOK §6.4。作成者: 要記入） |
| SSH鍵 | 管理者の個人鍵・Actionsのデプロイ専用鍵・VPSのdeploy key | 管理者の交代時に追加→確認→削除の順（RUNBOOK §5） |
| Dependabot | GitHub Actionsの依存更新のPR（#3・#4が未対応のメジャー更新） | 月に一度は見る。メジャー更新は別PRでpinごと確認 |

## 3. 無料・追加費用なし（確認だけ）

Discord（Botは無料）、Let's EncryptのTLS、Docker（Engine・Compose）、Ubuntu、PostgreSQL、Nginx、Go・Node.js・Next.jsなどのOSS（採用理由・版は[TOOLCHAIN](TOOLCHAIN.md)）。

## 4. ぬるのAPI使用額の確認

```
# 今月の使用額（ドル）と用途別（VPS上。nupsqlの定義は NUL_LAUNCH §6.1）
echo "select purpose, count(*), round(sum(cost_micro_usd)/1e6, 3) as usd from agent.api_usage where ts >= date_trunc('month', now() at time zone 'Asia/Tokyo') group by 1 order by 3 desc" | nupsql
```

- 見積もりの目安: 雑談1回 約1.5セント／Opus1件 15〜30セント／Web検索1回 1セント＋トークン／日次要約・内省はBatch（半額）で月に数十セント。
- **段階制御**: 70%で自発投稿・日誌・Batchを止め、90%でOpusとWeb検索を止め、100%で停止（次の付与日まで寝る）。DBが読めないときは停止側。
- 実際の請求・使用量は、Anthropic Consoleで確認する（DBの金額は単価表からの計算値で、請求額と断定しない）。

## 5. 契約・アカウントの引き継ぎ（卒業・交代のとき）

[RUNBOOK §12](RUNBOOK.md)の表に沿って、次の「名義・オーナー権限」を後任へ移す。**誰も操作できなくなる前に**（さくらのVPS・ドメインのレジストラ・Cloudflare・GitHub Organization・Anthropic（Team・Console）・Discord Developer Portalのアプリ・GitHubの`production`承認者・Owner権限のnu_appアカウント）。それぞれ「現在の名義（要記入）」を、このファイルか RUNBOOK §12 に書き込んでおく。
