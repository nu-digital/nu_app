# ぬる 本番運用開始ガイド（有効化の手順・実機テストのチェックリスト）

対象: AIメンバー「ぬる」（`services/agent`）を本番で有効にする運用責任者。
状態: 2026-11-28時点。実装（P1〜P3・リマインダー・内省）と、運用前の独立レビューへの対応は完了。**実際のDiscord・Claude APIには、一度も接続していない**（開発・CIは偽物で試験している）。この文書の手順は、有効化の前後に、運用責任者が実行・確認する。

- **本番デプロイの承認・API/Botの接続・有効化は、運用責任者の確認の後に行う**（開発側は行わない）。
- 秘密（Botのtoken・Claude APIキー・DBパスワード）は、チャット・Git・ログに出さない。VPSの`/srv/nu_app/.env`に、運用責任者が自分で書く。
- 仕様・決定: [nul-agent.md](design/nul-agent.md)・[DECISIONS](DECISIONS.md)（D15・NUL-1〜12）。日常の運用手順: [RUNBOOK §16](RUNBOOK.md)。独立レビューの結果: [handoffs/163-review.md](handoffs/163-review.md)。

---

## 0. 全体の流れ（約1〜2時間。止めながら進める）

| 段階 | 内容 | 状態 | 誰が |
|---|---|---|---|
| A | VPSのリソースと空きの確認 | 未 | 運用責任者（§1） |
| B | Discord Developer Portalの確認 | 未 | 運用責任者（§2） |
| C | Anthropic Consoleの設定（キー・上限・Web検索） | 未 | 運用責任者（§3） |
| D | 本番デプロイの承認（agentは無効モードで起動） | 未 | 運用責任者（§4） |
| E | バックアップ・復元の確認 | 未 | 運用責任者（§5） |
| F | 秘密の設置・DBロールの有効化・**停止状態で起動** | 未 | 運用責任者（§6） |
| G | 段階的な実機テスト（停止→静か→通常） | 未 | 運用責任者と開発側（§7） |
| H | 1週間の様子見（費用・ログ・DB） | 未 | 運用責任者（§8） |

**ポイント**: 最初は`control.mode = stopped`（全停止）で起動する。この状態でも、ぬるは**記録と過去ログの取り込みだけ**行い、返答・自発投稿・リアクション・Claude APIの呼び出し（返答用・要約用）はしない。取り込みが済み、設定（特に公開／非公開チャンネルの判定）を確認してから、`/nul quiet` → `/nul resume` と段階的に上げる。

---

## 1. VPSのリソースと空きの確認（A）

開発側からはVPSに入れない（SSH鍵を削除済み）。次を運用責任者が実行し、結果を共有してほしい（個人情報は含まれない）。

```
df -h /                                   # ディスクの空き
free -h                                   # メモリ
sudo du -sh /srv/nu_app/media /srv/nu_app/backups /var/lib/docker
sudo docker system df
ls /srv/nu_app/backups | wc -l            # バックアップの世代数
sudo docker stats --no-stream             # コンテナごとのメモリ・CPU
```

**判断の目安**

| 項目 | 目安 | 根拠 |
|---|---|---|
| ディスク | 空き20GB以上 | ぬるの記録は、メッセージ10万件でも約100〜200MB（索引込み）。バックアップは7世代分に膨らむ（1世代=DB+画像）ので、画像が多いほど効く |
| メモリ | 合計の上限は約2.8GB（db 640m・api 1g・bot 128m・agent 256m・web 384m・admin 384m）／VPSは約3.9GB | 余裕は約1GB。agentは通常数十MB。画像の縮小で一時的に200MB程度（同時2枚まで・12MP上限） |
| agentのメモリ上限 | 256m（`AGENT_MEM_LIMIT`） | 足りなければ384mへ。`docker stats`で有効化後に確認（§8） |

> 以前の実測では、ディスクは200GB中約6GB使用。agent追加による増加は、通常は誤差の範囲のはず。

---

## 2. Discord Developer Portalの確認（B）

[nul-agent.md §17](design/nul-agent.md)に沿って確認する。

| 確認 | 内容 |
|---|---|
| Bot | Botを作成済み。**Reset Tokenで発行したtokenは、パスワードマネージャーにだけ保存**（チャット・Gitに出さない） |
| 特権Intent | **MESSAGE CONTENT INTENT**・**SERVER MEMBERS INTENT**を有効（Presenceは不要）。無いとメッセージ本文・参加イベントが届かない |
| Botの権限 | 管理者権限を付けている前提（全チャンネルの閲覧のため）。ただし**コード側で、ぬるが使えるDiscord操作は「返信・リアクション・入力中・timesへの投稿・リマインダー」だけ**に制限している（削除・BAN・キック・ロール／チャンネル／設定の変更は実装していない） |
| 招待 | スコープ`bot`と`applications.commands`でサーバーに招待済み |
| ぬるのtimes | チャンネルを作成済み。そのID |
| Adminロール | `/nul stop|quiet|resume|status`を使えるロール。そのID。**Discordの権限ではなく、このロールのIDをコードが照合する** |
| ID | Guild ID・timesチャンネルID・AdminロールID（開発者モードでコピー）。**秘密ではない** |

---

## 3. Anthropic Consoleの設定（C）

| 設定 | 内容 |
|---|---|
| ワークスペース | ぬる用のワークスペース（組織全体のスコープにしない）。ほかでAPIを使わないなら、デフォルトのワークスペースでよい |
| APIキー | そのワークスペースのキーを発行。**有効期限は長め（90日など）**。発行したら、期限日を§6.5の方法でぬるに設定する（14日前から催促する）。キーはパスワードマネージャーにだけ保存 |
| 使用上限 | Console側でも**$60**に設定（コードの予算管理との二重の防御） |
| 支払い方法 | **登録しない**。**auto-reload（自動チャージ）も使わない**。クレジットが尽きたらぬるが止まるだけにする（追加課金なし） |
| Web検索 | 組織の設定でWeb検索を**有効化**する（無効だとぬるが「調べられなかった」と答える。ほかの機能は動く） |
| Batch API | 特別な設定は不要（要約・内省が使う） |

---

## 4. 本番デプロイの承認（D）

1. GitHub → Actions → deploy。**承認待ちのrunが複数ある**場合、古いもの（P1の途中のコミット）はReject。**最新のmainのコミット（このガイドを含むもの）**のrunだけ、内容・SHAを確認して「Approve and deploy」。
2. 承認すると、更新前バックアップ → migration（00013〜00020）→ 各サービス更新。agentは**秘密が未設定なので無効モード**で起動する。
3. 確認（VPS）:

```
sudo docker logs nu_app-agent-1 2>&1 | head        # 「無効モードで起動します」と、足りない設定の名前（値は出ない）
curl -s https://app.nu-digital.jp/healthz           # 既存サービスが正常
sudo docker compose -p nu_app -f /srv/nu_app/infrastructure/compose.prod.yaml --env-file /srv/nu_app/.env ps   # ※NU_TAGが要る（RUNBOOK §8）
```

4. 既存のサイト・ポータルが通常どおり動くことを確認する（agentは別コンテナで、他に影響しない）。

> 切り戻し: Actions → deploy → Run workflow → 戻したいSHA。DBのスキーマは前進のみで、前の版は新しいスキーマでも動く設計（RUNBOOK §8）。agentだけ止めるなら`docker compose stop agent`。

---

## 5. バックアップ・復元の確認（E）

ぬるの記録（メッセージ本文・ノート・要約・日誌・リマインダー）は、既存のDBバックアップ（毎日03:00 JST・7世代）に自動で含まれる。**DMの内容も含まれる**ので、バックアップの扱い（`/srv/nu_app/backups`は root・700）に注意する。

有効化の前に、次を実行して確認する（本番には触れない）。

```
sudo systemctl start nu-backup.service && journalctl -u nu-backup -n 30      # 手動で1世代作る
cat /srv/nu_app/backups/LATEST/manifest.txt                                   # agent_counts= に表ごとの行数が出る
sudo env POSTGRES_IMAGE='postgres:18.7@sha256:eb63372c1e05c755f55fa30cbd07aecc443676fc6c0fa90b9002d01537841e1a' \
         NUCTL_IMAGE="ghcr.io/nu-digital/nu_app-api:$(sudo cat /srv/nu_app/state/current_tag)" \
         /srv/nu_app/infrastructure/backup/restore-verify.sh                  # 末尾に「OK: …agent …」
```

- 復元の確認は、隔離した一時コンテナで、ダンプ・ロール・スキーマ・agentの行数・メディアの整合まで見る。CIでも毎回、同じ検査をしている。
- 実際の復旧（障害時）は[RUNBOOK §9.4](RUNBOOK.md)。**復旧後は`nu_agent`のパスワードを§16.2で再設定する**（ダンプにロールは含まれない）。
- **VPS外へのバックアップはしていない**（初期スコープ外）。ぬるの記憶（ログ・ノート）もVPSを失うと失う。VPS外コピーを加えるかは、運用責任者の判断事項（実装は小さい。希望があれば開発側で対応）。

---

## 6. 秘密の設置・DBロールの有効化・停止状態での起動（F）

### 6.1 `psql`の補助関数（VPS。以降の確認で使う）

```
nupsql() { sudo bash -c 'set -a; . /srv/nu_app/.env; set +a; T=$(cat /srv/nu_app/state/current_tag); env NU_TAG=$T docker compose -p nu_app -f /srv/nu_app/infrastructure/compose.prod.yaml --env-file /srv/nu_app/.env exec -T db psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" "$@"' _ "$@"; }
echo "select count(*) from agent.messages" | nupsql -At     # 0 と出れば、migration済みで動いている
```

### 6.2 まず「全停止」を保存してから起動する（重要）

```
echo "insert into agent.settings (key, value) values ('control.mode', '\"stopped\"') on conflict (key) do update set value = excluded.value" | nupsql
```

### 6.3 `/srv/nu_app/.env` に追記（運用責任者が自分で。root・600のまま）

```
NUL_DISCORD_TOKEN=（Botのtoken）
NUL_ANTHROPIC_API_KEY=（Claude APIキー）
NUL_DB_PASSWORD=（英数字32文字以上のランダム値。例: openssl rand -hex 24）
NUL_GUILD_ID=（Guild ID）
NUL_TIMES_CHANNEL_ID=（timesチャンネルID）
NUL_ADMIN_ROLE_ID=（AdminロールID）
```

### 6.4 DBロール`nu_agent`の有効化 → 再起動

[RUNBOOK §16.2](RUNBOOK.md)のコマンド（`ALTER ROLE nu_agent LOGIN PASSWORD …`）を実行し、`up -d agent`で再起動する。

```
sudo docker logs nu_app-agent-1 2>&1 | tail -20     # 「Discordに接続しました」。本文・tokenは出ない
```

### 6.5 APIキーの期限を設定する

```
echo "insert into agent.settings (key, value) values ('apikey.expires', '\"2027-02-25\"') on conflict (key) do update set value = excluded.value" | nupsql    # 日付はConsoleで確認した期限
```

### 6.6 この時点（全停止）で確認すること

- Discordで`/nul status`（Adminロール）→ 「停止中」・取り込み済み件数が増えていく（過去ログは1ページごとに1秒待つため、ゆっくり）。Adminでない人が実行すると断られる。
- ぬるに話しかけても**何も返さない**（リアクションも付かない）。timesに自発投稿しない。
- `echo "select count(*) from agent.api_usage" | nupsql -At` が **0**（Claude APIを1回も呼んでいない）。
- 取り込みが一段落（`/nul status`の件数が落ち着く）したら、§7へ。

---

## 7. 段階的な実機テスト（G）

> 各テストで「期待」と違えば、すぐ`/nul stop`して、結果を共有する。**テストは、運営だけの非公開チャンネルと、テスト用の公開チャンネル（またはDM）で行う**。実在のメンバーの個人情報は使わない（架空の内容で試す）。

### 7.1 公開／非公開の判定（最重要。全停止のうちに確認）

```
echo "select channel_id, bool_or(is_public) as public, count(*) from agent.messages where kind='guild' group by 1 order by 3 desc limit 30" | nupsql
```

- [ ] 運営だけのチャンネル・ロール限定のチャンネル・非公開スレッドが`public = f`
- [ ] 全員が見られるチャンネルが`public = t`
- [ ] 想定と違うものは、Discordのチャンネル権限（@everyoneの「チャンネルを見る」）を確認する。判定は5分ごとに更新（新しいメッセージから反映）

### 7.2 静かモード（返答はするが、自分からは投稿しない）

`/nul quiet`

- [ ] テスト用の公開チャンネルで`@ぬる こんにちは` → 返信が付く（誰にも通知は飛ばない）。メンションなしの発言には応えない
- [ ] timesで話しかける → 応える。timesのスレッド内はメンションが要る
- [ ] DMで話しかける → 応える
- [ ] 画像つきでメンション → 画像の内容に触れる
- [ ] `@ぬる 本気出して〇〇` → 応える（Opus。1日3回まで）
- [ ] timesへの自発投稿・リアクションが**付かない**
- [ ] `echo "select model, purpose, cost_micro_usd from agent.api_usage order by id desc limit 10" | nupsql` で、sonnet（chat）・opus（think_hard）の記録と金額が出る。Consoleの使用量とおおむね一致

### 7.3 記憶・プライバシー

- [ ] 公開チャンネルで「私は猫が好き。覚えておいて」→ ノートに入る。`echo "select id, subject_user_id is not null, private, content from agent.notes order by id desc limit 5" | nupsql`
- [ ] **DMで**「ひみつの悩みを覚えておいて」→ ノートが`private = t`。別の人（または公開チャンネル）で、その人のノートを聞いても**出てこない**
- [ ] 公開チャンネルで「さっきの私のメモ見せて」 → DMで作ったノートは出ない
- [ ] **非公開チャンネル**に「テスト用の合言葉ホゲホゲ」と書き、公開チャンネルで「ホゲホゲで過去ログ検索して」→ **見つからない**と答える
- [ ] 公開チャンネルの発言（合言葉ピヨピヨ）は、検索で見つかる
- [ ] 「私のことを全部忘れて」→ 「メモ○件消した」。ノートが消える。`忘れてない？`のような文では発動しない
- [ ] 公開チャンネルで発言を**削除** → そのメッセージを元にしたノートが消える（`select count(*) from agent.notes where source_message_id = <削除したID>`）
- [ ] 「明日の10時にリマインドして」→ 登録。`select id, remind_at, private from agent.reminders`。時刻になると、依頼した場所に、依頼した本人だけにメンションして届く。DMで頼んだものは、公開チャンネルの「リマインダー見せて」に**出ない**
- [ ] 全停止中のリマインダーは届かず保留（再開後に「遅れてごめん」付きで届く）

### 7.4 全停止・予算

- [ ] `/nul stop` → メンションに応えない・リアクションなし・自発投稿なし。`/nul status`だけは動く。`/nul resume`（静かではなく通常に戻る点に注意）
- [ ] （予算のテスト）寝る動作の確認:

```
echo "insert into agent.settings (key, value) values ('budget.exhausted_until', '\"2099-01-01T00:00:00+09:00\"') on conflict (key) do update set value = excluded.value" | nupsql
```
  メンションすると、返答の代わりに💤のリアクションだけが付く（Claude APIは呼ばない）。**確認したら必ず解除**:
```
echo "delete from agent.settings where key = 'budget.exhausted_until'" | nupsql
```

### 7.5 通常モード（自発投稿・リアクション）

`/nul resume`

- [ ] **`/nul resume`の直後（1分以内）に、ぬるがtimesへ最初の挨拶を1回だけ書く**（固定文ではなく自分で考えた文章。内容がキャラ・団体の実態に合っているか確認。再起動しても二度は書かない）
- [ ] 1日以内に、timesへ自発投稿が最大3回（深夜に多め・間隔90分以上）。内容がキャラ（§2.5の線）から外れていない
- [ ] リアクションがたまに付く（1日最大10件）。`reactions.enabled = false`で止められる（RUNBOOK §16.5）
- [ ] 新メンバー（テスト用アカウント）の参加で、timesに一言（メンションなし）
- [ ] 金曜21時以降に週報、月初に月報（材料の要約が溜まってから）。日次要約・内省・日誌は、翌日以降にConsole（Batches）とDB（`agent.summaries`・`agent.diary`）で確認

### 7.6 緊急時

- [ ] `/nul stop`が効かない想定で、`sudo docker stop nu_app-agent-1`で止まること、`up -d agent`で戻ることを一度試す（RUNBOOK §16.3）

---

## 8. 有効化後の1週間（H）

毎日1〜2回、次を見る（ぬるの管理画面ができるまでは手動。設計は[design/nul-admin-dashboard.md](design/nul-admin-dashboard.md)）。

```
# 今月の使用額（ドル）と、用途別
echo "select purpose, count(*), round(sum(cost_micro_usd)/1e6, 3) as usd from agent.api_usage where ts >= date_trunc('month', now() at time zone 'Asia/Tokyo') group by 1 order by 3 desc" | nupsql
# DBの大きさ
echo "select pg_size_pretty(sum(pg_total_relation_size(c.oid))) from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname='agent'" | nupsql -At
# 件数
echo "select (select count(*) from agent.messages) msgs, (select count(*) from agent.notes) notes, (select count(*) from agent.summaries) sums, (select count(*) from agent.diary) diary, (select count(*) from agent.reminders) rems" | nupsql
sudo docker stats --no-stream nu_app-agent-1      # メモリ
sudo docker logs --since 24h nu_app-agent-1 2>&1 | grep -i "error\|warn" | head -30    # エラー（メッセージ本文は出ない）
```

**費用の目安**（月$60の予算内）: 雑談は1回約1.5セント・Opusは1件15〜30セント・調べもの日誌は1日数セント・要約と内省はBatch（半額）で月に数十セント程度。**70%（$42）で自発投稿が減り、調べもの日誌・要約が止まる／90%で Opus 停止／100%で停止**。Console側の上限$60も二重に効く。

---

## 9. 既知の制限（隠さずに残す）

- 全停止中でも、**メッセージの記録と過去ログの取り込みは続く**（仕様）。
- 過去ログの取り込みは、**アーカイブ済みの公開スレッドだけ**で、非公開スレッド・DMは過去分を取り込まない（リアルタイムでは記録する）。
- 「私のことを忘れて」で消えるのは、ノート・リマインダー・親密度。**サーバー上の発言の記録そのものと、それから作った要約は残る**（仕様）。必要ならDBで`messages.author_id`を指定して削除する。
- チャンネルの公開→非公開の変更は、新しいメッセージから反映される。すでに作られた要約・日誌・ノートに残った内容は自動では消えない（RUNBOOK §16.14）。
- 個人情報の検出は「形」（メール・電話・長い数字・パスワード表現）で、氏名などは判定できない。プロンプトの指示と併用している。
- 予算期間の付与日（既定は毎月1日）は、実際のクレジット付与日と合っているか、Consoleで確認する。違う場合は開発側で設定を追加する。
- 並行リクエストでは、予算の段階を少し（数セント）超えることがある。最終防衛はConsole側の$60上限。
- ぬるの稼働状況・使用額・記憶量をnu_app上で見る画面は未実装（設計のみ）。当面は§8のSQLで確認する。
