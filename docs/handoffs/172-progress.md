# 172 進捗: ドキュメント全体の整理

## 状態（事実）
- README.mdが空（0バイト）になっていた（#125のときの編集ミス。CLAUDE.mdが「最初にREADMEを読む」としていたのに入口が無かった）→ 書き直した。
- 2つの独立した監査（仕様系・運用系。別のエージェントが読み取り専用で実装と突き合わせた）の指摘を反映した。
- 新設: docs/README.md・START_HERE・FEATURES・NUL_OVERVIEW・HISTORY・SERVER_SETUP・COSTS_AND_CONTRACTS・AI_GUIDE・handoffs/README・LATEST。更新: ARCHITECTURE・DATABASE・API・AUTHZ・TESTING・TOOLCHAIN・REQUIREMENTS・DECISIONS・TASKS・BACKLOG・RUNBOOK・OPERATIONS・LOCAL_DEV・WSL_SETUP・DEVELOPMENT・CLAUDE_HANDOFF・NUL_LAUNCH・AGENTS・CLAUDE・design/*・legacy-baseline。
- 実測: `node scripts/check.mjs` PASS（リンク切れなし）。文書だけの変更なのでデプロイは動かない。

## 未実施・要確認（運用責任者）
- `COSTS_AND_CONTRACTS.md`の「（要記入）」（VPS・ドメイン・Cloudflare・GitHub・Teamの費用と契約者・更新日）。RUNBOOKの「（要記入）」。
- `SERVER_SETUP.md`の`※要確認`（構築時の記録が文書に無かった箇所: リポジトリの取得方法（core.sshCommand）・sudoers・backup.envの実際の値）を、本番のサーバーで実際の設定と突き合わせる（`sudo git -C /srv/nu_app config --get core.sshCommand`・`sudo ls -l /etc/sudoers.d/`・`sudo cat /etc/nu_app/backup.env`（値は出さない））。SERVER_SETUPは実際に新しいサーバーで試していない。
- ブランチ保護の必須チェックに`go / services/agent`・`ops / *`が無い（DEVELOPMENT.md）。

## コード・設定の変更が要る項目（別PR。デプロイを伴う）
- `backup.sh`の`COMPOSE_FILE`既定値が`/srv/nu_app/compose.prod.yaml`（実際は`infrastructure/compose.prod.yaml`）。本番は`backup.env`で上書きしている可能性。
- `services/bot/bot`（コミットされたELF実行ファイル）の削除・.gitignore。
- `scripts/check.mjs`でREADME.mdが空でないことを検査する。
- AUTHZの通知IDの重複の整理。

## 次の一手
- 運用責任者が上記の確認と記入。コード・設定の変更PR。
