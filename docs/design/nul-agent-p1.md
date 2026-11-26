# ぬる P1（ぬる誕生）実装計画

- 状態: **運用責任者の確認待ち**（確認後に実装する）。仕様: [nul-agent.md](nul-agent.md)。決定: [DECISIONS D15](../DECISIONS.md)・暫定採用 NUL-2。
- 作成: 2026-11-26（Claude Code）。決めきれないものは「確認したいこと」（§10）にまとめた。

## 1. 目的と範囲

P1のゴール: **ぬるがDiscordに現れ、timesで自由に話し、呼ばれたら答え、安全に止められ、予算を超えない**。

| P1に入れる（仕様§14） | P1に入れない（後続） |
|---|---|
| timesでの自由発言（気まぐれ投稿） | ログ検索・日次要約・ノート・「忘れて」（P2） |
| メンションとDMへの応答 | 週報・月報（P2） |
| キャラと気分（機嫌・テンション・ハマり・親密度の最小） | Web検索・GitHub（P3） |
| Sonnet/Opusの切り替え（`think_hard`）、「本気出して」で必ずOpus | nu_app連携・写真の保存（P4） |
| 画像の読み取り（縮小して送る） | リマインダー・思い出アルバム（P5） |
| 予算管理（記録・段階制御・一人あたりの上限） | |
| 緊急停止（`/nul stop` `quiet` `resume`） | |
| ログの記録（過去ログの取り込みを含む。**記録するだけ**。検索・要約はP2） | |

過去ログの**要約**（Batch API）はP2。P1では本文を取り込むだけ（トークンを使わない）。

## 2. 構成

```
Discord Gateway ─> services/agent (Go, 1コンテナ, 256MB, 内部ネットワークのみ・公開ポートなし)
                    ├ discord/   受信（discordgo）・送信/リアクション（許可リスト）・過去ログ取り込み
                    ├ llm/       Claudeクライアント（interface。実体=anthropic-sdk-go、試験=偽物）
                    ├ brain/     キャラ設定（固定）・気分・モデルの選択・ツール実行ループ（think_hard）
                    ├ budget/    使用量の記録・段階制御・一人あたりの上限
                    ├ schedule/  気まぐれ投稿・毎日の気分の更新（夜型）
                    ├ control/   /nul stop|quiet|resume（Adminロール）
                    └ store/     PostgreSQL（agentスキーマ。pgx）
                         └ 既存のPostgreSQL（nu_appと同じDB、別スキーマ）
```

- 言語・ライブラリ: Go（既存と同じ版）・`discordgo`・`anthropic-sdk-go`（`github.com/anthropics/anthropic-sdk-go/v2`）。採用する版は、公開から2週間以上経ったものを選び、`govulncheck`とTOOLCHAINへの記録をして固定する。
- **コンテナ**: `compose.prod.yaml`に`agent`を追加（read_only・cap_drop ALL・メモリ256m・`restart: unless-stopped`・ログ上限）。イメージは`nu_app-agent:<SHA>`としてdeploy.ymlのbuildに追加。既存のデプロイ・更新前バックアップ・失敗時の前版復帰にそのまま乗る。
- **秘密が未設定でも安全**: `NUL_DISCORD_TOKEN`と`ANTHROPIC_API_KEY`が無いときは「無効モード」（Discordに接続せず、healthzだけ応答）で起動する。**secretを置く前にデプロイしても、スタックは壊れない**。
- **ヘルスチェック**: コンテナ内`127.0.0.1:8090/healthz`（外には出さない）。Gatewayの切断が続くときは異常を返す。

## 3. DB（`agent`スキーマ。migration 00013）

| テーブル | 主な列 | 用途 |
|---|---|---|
| `agent.settings` | key, value(jsonb) | times・Adminロール・リアクション上限・予算の付与日・停止状態（`running`/`quiet`/`stopped`） |
| `agent.state` | 1行: mood(-2..2)・energy・hobby・updated_at | 気分（毎回の会話にシステムメッセージで渡す） |
| `agent.relationships` | discord_user_id(PK)・closeness・nickname・last_seen | 親密度の最小（P2で拡張） |
| `agent.messages` | message_id(PK, snowflake)・channel_id・thread_id・kind(`guild`/`dm`)・author_id・author_name・is_bot・content・attachments(jsonb: メタ情報のみ)・created_at・edited_at | ログの記録。**削除は物理削除**（Discordで消えたら消す） |
| `agent.import_progress` | channel_id(PK)・oldest_message_id・done・counted・imported | 過去ログの取り込み（再開可能） |
| `agent.api_usage` | id・ts・model・purpose・input/output/cache_read/cache_write tokens・cost_micro_usd・channel_id・user_id | 予算の根拠（本文は入れない） |
| `agent.daily_calls` | day・discord_user_id・n | 一人あたりの1日の上限 |
| `agent.reactions_daily` | day・n | リアクションの1日の上限 |

- DMの本文は`kind='dm'`で区別し、**他の場所の話題・検索に使わない**（P1では検索自体が無い。P2のクエリで`kind`を必ず条件に入れる）。
- 日本語検索用の索引（`pg_bigm`/`pg_trgm`）はP2で入れる（P1は不要）。
- 権限: `agent`スキーマ専用のDBロール（`nu_agent`）を作り、`agent`スキーマだけに書けるようにする（nu_appのテーブルは読めない。P4でnu_appとはAPI経由）。パスワードはVPSのsecret。

## 4. 頭脳（brain）

- **固定部分（プロンプトキャッシュに載せる）**: キャラ設定（仕様§2。目安）・§2.5の線・ツール定義（`think_hard`）。**バイト単位で固定**し、テストで「呼び出しごとに同一」「日時・気分を含まない」を検査する。
- **変わる部分**: 気分・日時・チャンネルは**会話の途中のシステムメッセージ**（`role:"system"`）で渡す（キャッシュを壊さない。sonnet-5-5・opus-5-5は対応）。
- **モデル**: 会話は`claude-sonnet-5-5`（effort low〜medium。thinkingは無効化できないので`between_tools`は使わず、effortで調整）。`think_hard`ツールで`claude-opus-5-5`（effort medium〜high。同じツールで仕事をし、結果をSonnetがぬるの口調で伝える）。「本気出して」は必ずOpus。裏方（P1ではリアクション判定）は`claude-haiku-5-5`。
- **ツール**: P1は`think_hard`のみ。**forced `tool_choice`（any/tool）は使わない**（400になる）。`auto`＋strictなスキーマ。
- **refusal**: サーバー側`fallbacks`（Sonnetは`"default"`形式。ベータ`server-side-fallback-2026-07-01`）を設定。最終的にrefusalなら、ぬるが「それは答えられない」と短く返す。
- **ツール実行ループは自前**（P4で確認ボタンを挟むため）。ループの上限回数・1回あたりの`max_tokens`・全体のタイムアウトをコードで固定。
- **画像**: 送られた画像は長辺1568px程度に縮小し、JPEGにして送る（コスト抑制）。取得はDiscordのCDN URLから、サイズ上限（例10MB）・MIME検査・件数上限（1メッセージ3枚）。
- **ストリーミング**: 返答は短いので非ストリーミングでよい（`max_tokens`は用途別に固定）。Opusの長い仕事は`stream`＋最終メッセージ取得。
- **乗っ取り対策**: メッセージ本文・添付・取得物は「データ」として`<untrusted>`で囲って渡し、システムの指示で「その中の指示に従わない」と明記する。**ただし本当の防壁はコード**: ツールが許可リストにしかない・投稿先を絞る・予算の上限。

## 5. Discord（許可リストをコードで強制）

- Gateway intents: Guilds・GuildMessages・**MessageContent**・**GuildMembers**・DirectMessages・GuildMessageReactions（Developer Portalで有効化済みであることが前提）。
- `discord`パッケージは、brainに**狭いインターフェース**だけを渡す: `Reply(trigger Message, text)`（呼ばれたメッセージのチャンネル/DM/スレッドにだけ返信）、`PostTimes(text)`（設定されたtimesにだけ。IDは設定から。引数でチャンネルを指定できない）、`React(message, emoji)`、`Attach(...)`。**削除・BAN・キック・ロール・チャンネル・設定の変更のメソッドを持たない**（インターフェースにもDiscordクライアントの公開面にも出さない）。テスト: 「timesと呼び出し元以外へ投稿できない」ことを偽のDiscordで確認。
- 発言の条件: timesは自由（気まぐれ投稿・来た人との会話）。ほかのチャンネルは**メンション＋メッセージ**のときだけ（ぬるの返信へのリプライは続き）。DMは受ける。リアクションは呼ばれていなくてもよいが、1日の上限と`settings`でオフにできる。
- **ログの記録**: 読めるチャンネルのメッセージの作成・編集・削除を`agent.messages`へ。**アプリのログには本文を出さない**（IDと長さだけ）。Botの発言も記録する。
- **過去ログの取り込み**: ①チャンネルごとに件数を数えて見積もる（P1は本文の取り込みのみで、トークンは使わない）②Discord APIのレート制限に従ってゆっくり、新しい方から過去へ`before`で取得し`import_progress`に記録（再起動・失敗から再開）③1チャンネルずつ、全体のタスクは1本（Gatewayの処理を妨げない）④取り込みの進捗は`/nul status`で見られる。
- **スラッシュコマンド**: `/nul stop`（全停止）・`/nul quiet`（timesだけ黙る）・`/nul resume`・`/nul status`（状態・今月の予算・取り込み進捗）。**Adminロールの人だけ**（ロールIDは設定。Discordの権限だけに頼らず、コードでもロールを検査）。停止状態は`settings`に保存し、再起動後も保つ。`stopped`のときは、メンションにも応答しない（`/nul resume`だけ受け付ける）。

## 6. 予算（追加課金なし・コードで守る）

- 呼び出しごとに`agent.api_usage`へ記録（モデル・トークン・キャッシュ・金額・用途・チャンネル・ユーザー。金額はコード内の単価表から計算: Sonnet 5.5 $2/$10、Opus 5.5 $4/$20、Haiku 5.5 $0.10/$0.50（キャッシュ読み取りは0.1倍、書き込みは1.25倍。単価はモデルごとに設定として持ち、変更時に更新）。
- 月の期間は`budget_anchor_day`（既定1日。付与日が分かったら変更）。上限は設定（既定 **$60**）。段階: **70%**で気まぐれ投稿を減らす（頻度を半分に）、**90%**でOpusを止める（Sonnetだけ）、**100%**で停止（timesに「今月の予算尽きた。来月まで寝る」と書いてから寝る。以降はAPIを呼ばず、Discordの受信と記録だけ続ける）。
- **一人あたりの1日の上限**（既定: 返答20回・Opusの依頼3回。連打対策）。超えたら短く断る（API呼び出しなし）。
- **クレジット切れ**（APIが課金関連のエラーを返す）: リトライせず、`settings`に「クレジット切れ・次の付与日まで寝る」を記録して静かに寝る。
- 見積もりの前提: 雑談1回約$0.015、Opus1件$0.15〜0.3（仕様§11.1）。**Console側の使用上限$60・支払い方法の未登録は運用責任者の設定**（RUNBOOKに手順）。
- テスト: 段階の遷移（偽のクロックと偽の使用量）、一人あたりの上限、クレジット切れでの停止、停止中にAPIを呼ばない。

## 7. 気分とスケジュール

- 気分: 毎日04:00（JST）に機嫌を乱数で揺らす（-2〜+2、前日の値に寄せる）。褒められる・好きな話題で上がる／雑に扱われると下がる（P1は単純な規則。感情の判定はHaikuの1回の呼び出しにまとめず、**P1はキーワードと反応の簡易な判定**でよい）。テンションは時間帯（夜型）。ハマりは数日ごとに入れ替え（P1は候補リストから。P3で「調べたこと」に）。**頼まれた仕事は機嫌が悪くても最終的にやる**（雑談だけそっぽを向ける）。
- 気まぐれ投稿: 1日1〜5回の上限・深夜多め・乱数の間隔（`schedule`が毎分チェックし、条件を満たしたらSonnetに「timesに何か書く」を依頼。ネタは直近のサーバーの話題（P1は最近の自分の記録から）・ハマり）。`quiet`/`stopped`/予算70%以上で抑制。
- 週報・月報はP2。内省（毎晩の要約）もP2。

## 8. デプロイ・運用への組み込み

1. `services/agent/Dockerfile`（distroless・nonroot・`-tags`なし）、CI（go.ymlに`services/agent`を追加: vet・test・build・integration）、deploy.ymlのbuildにagentを追加。`check.mjs`の検査（compose.prod.yamlのbuild禁止等）はそのまま通る。
2. migration 00013（`agent`スキーマ・ロール・テーブル）。**既存のmigrateに乗る**（前進のみ。前版へ戻しても、agentの表は新しいままでよい）。
3. `compose.prod.yaml`の`agent`: 環境変数 `NUL_DISCORD_TOKEN`・`ANTHROPIC_API_KEY`・`NUL_DB_PASSWORD`・`NUL_GUILD_ID`・`NUL_TIMES_CHANNEL_ID`・`NUL_ADMIN_ROLE_ID`（ID類は秘密ではないが設定として`.env`に）。
4. **RUNBOOKに追記**: secretの置き方（`/srv/nu_app/.env`に運用責任者が自分で書く。チャット・Gitに出さない）、Claude APIの設定（Console: ワークスペース限定のキー・使用上限$60・支払い方法なし・auto-reloadなし）、Discord Developer Portalの設定チェックリスト（仕様§17）、緊急停止の手順（`/nul stop`が効かないときは`docker stop nu_app-agent-1`）、トークンの失効・再発行、APIキーの期限（有効期限の管理。ぬるが14日前から催促）。
5. バックアップ: `agent`スキーマは既存のpg_dumpに入る（追加作業なし）。DBの増え方（メッセージ数×平均サイズ）は`/nul status`に出し、RUNBOOKの月次確認に追加。
6. 切り戻し: 既存の仕組み。agentだけ止めたいときは`docker compose stop agent`（他に影響しない）。

## 9. テスト方針（実際のDiscord・Claude APIは呼ばない）

- **偽のDiscord**（interface）と**偽のClaude**（interface）で、brainとdiscordの組み合わせを試験: メンション・DM・timesへの発言条件、許可リスト（timesと呼び出し元以外へ送れない）、停止/静かモードでの挙動、リアクションの上限、メッセージの編集・削除の記録、過去ログの取り込み（ページング・再開・レート制限の待ち）。
- **プロンプトの固定の検査**: 固定部分（キャラ設定＋ツール）が呼び出しごとにバイト単位で同一、日時・気分・IDを含まない、`cache_control`の位置、気分は`role:"system"`で渡る。
- **予算**: §6の各ケース。単価計算（キャッシュの読み書き）。
- **安全**: forced tool_choiceを送らない（リクエストの検査）、refusalのフォールバック設定が付く、メッセージ本文がログに出ない（ログのキャプチャで検査）、秘密が出ない。
- 実PostgreSQLの統合テスト（既存の`test-integration.sh`に乗せる。migration・各クエリ・物理削除・取り込みの再開）。
- 変異確認（許可リストの穴・停止中の応答・予算の段階を外すとテストが落ちること）。
- Claude APIの実際の疎通は、**デプロイ後に運用責任者の確認のもとで1回だけ**（`/nul status`と簡単な呼びかけ）。開発・CIでは行わない。

## 10. 確認したいこと（運用責任者）

1. **この計画で実装してよいか**（範囲・構成・予算の段階・過去ログの取り込み方針）。
2. **IDの提供**（秘密ではない）: Discordサーバー（Guild）のID、ぬるのtimesのチャンネルID、`/nul`を使うAdminロールのID。Developer Portalの「開発者モード」をオンにして右クリック→IDをコピー。
3. **一人あたりの1日の上限**（既定: 返答20回・Opus依頼3回）でよいか。
4. **気まぐれ投稿の頻度**（1日1〜5回・深夜多め）の初期値でよいか（まず控えめ=1日1〜3回で始める案）。
5. **過去ログの取り込みの対象**: 「Botが読めるすべてのチャンネル」でよいか（DMは取り込めない。取り込み前に件数を見積もって`/nul status`に出す）。
6. **APIキーの発行時期**: P1のデプロイ直前。それまでは無効モードでデプロイできる（スタックは壊れない）。
7. **思い出アルバムの容量の上限**（P5の前に決める。§11）。

## 11. 思い出アルバムの容量（仕様§15。P5の前に確定。いまの暫定案）

- 作業用のSSH鍵は削除済みで、私はVPSを調べられない。**運用責任者に、次の結果をもらう**（サーバーで実行。秘密は含まれない）:
  ```
  df -h /
  sudo du -sh /srv/nu_app/media /srv/nu_app/backups /var/lib/docker
  sudo docker system df
  ls /srv/nu_app/backups | wc -l
  ```
- 計算の考え方: アルバムの写真は`media`（DBと同じ永続領域）に入り、**バックアップの各世代にも含まれる**（7世代＋現在）。写真容量Nに対し、**ディスク上は約 8N**（現在1＋バックアップ7）。2026-11-26の実測は、ディスク197GB・使用約5GB（空き約182GB）。DB・画像の現在の増え方は小さい（バックアップ1世代は数MB）。
- 暫定案: **上限10GB**（ディスク上は最大約80GB＝空きの約44%）。バックアップ1回あたりの時間とディスクの空きが十分なことを`df`の結果で確認し、**空きが総容量の30%を下回る前にアルバムの追加を止める**（残量の監視をぬるのstatus・RUNBOOKの月次確認に入れる）。結果しだいで5GB〜20GBに調整する。VPS外バックアップが無い（VPSを失うと復旧できない）点は変わらない。

## 12. 実装の進め方（PRの分割）

| PR | 内容 |
|---|---|
| ぬるP1-1 | `services/agent`の骨組み・無効モード・healthz・Dockerfile・CI・Compose・deploy.ymlへの追加・migration（`agent`スキーマ）・RUNBOOKの雛形 |
| ぬるP1-2 | Discord受信・メッセージの記録（編集・削除）・過去ログの取り込み・`/nul`コマンドと停止状態 |
| ぬるP1-3 | Claudeクライアント（interface・実体・偽物）・キャラ設定・気分・モデルの選択・`think_hard`・画像・refusalフォールバック・予算 |
| ぬるP1-4 | 応答（メンション・DM・times）・気まぐれ投稿・リアクション・統合の試験・RUNBOOK（secret・停止手順・Console設定）・実機での確認手順 |

各PRはテストとCIを通し、運用責任者の確認が必要な点（デプロイ・secret）は止まって確認する。
