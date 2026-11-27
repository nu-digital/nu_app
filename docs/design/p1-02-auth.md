# P1-02 認証フローの設計（暫定仕様つき）

> **状態（2026-11-28）: 実装済み。これは設計の経緯と理由の記録（履歴）**で、実装の正本はコードと[docs/README.md](../README.md)の各正本文書（[ARCHITECTURE](../ARCHITECTURE.md)・[DATABASE](../DATABASE.md)・[API](../API.md)・[AUTHZ](../AUTHZ.md)）。以下の「暫定」「提案」「未実装」などの表現は、書かれた当時のもの。

状態: 設計（2026-11-21、Opusによる設計レビュー反映済み）。**ユーザー確定事項は[DECISIONS](../DECISIONS.md)のD01〜D13と、認可表の承認のみ**。この文書の数値・方式は、特記がない限り**暫定採用**（DECISIONSの「暫定採用」に理由・代替案・影響を記録。運用責任者が実際に触って判断する）。

## 範囲

- 公開: `POST /auth/login`、`POST /auth/accept-invite`、`POST /auth/reset-password`
- 認証済み: `POST /auth/logout`、`GET /auth/me`
- 管理: `POST /admin/users`、`POST /admin/users/{id}/invite-link`、`POST /admin/users/{id}/reset-link`、`PATCH /admin/users/{id}/status`
- 非対象（P1-03）: Editor/Admin付与、Owner移譲、緊急復旧CLI。ログインUI（P1-04）。

認可は`internal/authz`（承認済み）を使う。応答はAPI.mdの形式（JSONエラー`{error:{code,message,requestId}}`）。

## 脅威と対策

| 脅威 | 対策 | テスト |
|---|---|---|
| パスワード総当たり・ユーザー列挙 | ログイン失敗は常に同じ文言・同じステータス（401）。存在しない/停止/招待中/誤パスワードを区別しない。未存在ユーザーでもダミーのArgon2id検証を行い時間差を減らす。**IP**・**(ID,IP)**・**ID**の失敗回数でレート制限（429、`Retry-After`）。成功で当該IDの失敗カウントを消す | 同一応答、制限超過で429、ID別/IP別 |
| 弱いパスワード | 10〜128文字、ログインIDと同一不可、同一文字の繰り返し不可、ごく一般的なパスワード一覧に一致不可。Argon2idで保存（平文・可逆暗号なし）。ハッシュ計算の同時実行数を制限（4GB VPS保護） | ポリシー境界、ハッシュ形式 |
| セッション固定 | ログイン成功時に**常に新しいセッション**を発行（受け取ったCookieは使わない）。パスワード変更/再設定/停止で当該ユーザーの全セッションを失効 | 旧セッション無効化 |
| セッション盗難 | 32バイト乱数、DBにはSHA-256のみ。Cookieは`HttpOnly; Secure; SameSite=Lax; Path=/`（ローカルHTTP開発のみ`Secure`を設定で外せる）。絶対期限14日・アイドル7日。失効済み・期限切れ・停止中は拒否。ログアウトで失効 | 期限、失効、停止 |
| CSRF | 状態変更（POST/PUT/PATCH/DELETE）は、①`Origin`（なければ`Referer`）が許可オリジンと一致、かつ ②認証済みは`X-CSRF-Token`がセッション由来の値と一致。CSRFトークン = `hex(SHA-256("nu-csrf-v1:" + セッションtoken))`（`/auth/me`・ログイン応答で返す。HttpOnlyなCookieの生値はJSから読めないので、攻撃者は計算できない）。未認証のログイン等もOriginを検証（ログインCSRF対策） | Origin不一致/欠落、トークン不一致 |
| 招待・再設定トークンの再利用・並行利用 | トークンは32バイト乱数・SHA-256保存・期限付き・使い捨て。**使用は1つのUPDATE（`used_at IS NULL AND revoked_at IS NULL AND expires_at > now()` の条件付き）でアトミックに確保**し、同時に2人が使っても成功は1件。再発行時は古い有効トークンを失効 | 並行使用で成功1件、期限切れ、使用済み、失効後 |
| 停止後のアクセス | 停止（PATCH status=disabled）は同一トランザクションでユーザーの全セッションを失効。セッション認証は毎回`status=active`を確認 | 停止後の既存セッションが使えない |
| 情報漏えい | パスワード・token・Cookie・DSNをログ・監査・エラーに出さない。トークン無効の応答は理由を区別しない（無効/期限切れ/使用済みは同一文言）。`Cache-Control: no-store` | ログ/応答の検査 |
| タイミング | トークン・CSRFの比較は`subtle.ConstantTimeCompare`（SHA-256後の比較のため実質的に影響は小さいが統一） | – |

## フロー

1. **招待**: Admin/Ownerが`POST /admin/users`（loginId・表示名）→ ユーザー(invited)＋プロフィール＋招待トークン（purpose=invite）を1トランザクションで作成し、`inviteUrl`（`<APP_BASE_URL>/invite#token=…`。**フラグメント**）を**一度だけ**返す。再発行は`invite-link`（invited状態のみ。古いトークンは失効）。
2. **招待受諾**: `POST /auth/accept-invite {token, password}` → トークンをアトミックに使用済みにし、パスワードを保存し、status=active・activated_at設定、監査を記録。**自動ログインはしない**（応答は成功のみ。ログインは通常のログインで行う）。
3. **ログイン**: `POST /auth/login {loginId, password}` → 検証後に新規セッション。応答に`csrfToken`。
4. **再設定**: 本人が運営者へ連絡 → Admin/Ownerが`reset-link`（active状態のみ。管理者権限の対象はOwnerのみ=認可表）→ `POST /auth/reset-password {token, password}` → パスワード更新・**全セッション失効**・トークン使用済み。メール送信はしない。
5. **停止/再有効化**: `PATCH /admin/users/{id}/status {status}`。停止は全セッション失効。自分自身の停止・最後のOwnerの停止は拒否（最後のOwner保護の本実装はP1-03だが、ここで停止対象にOwnerが含まれる場合はOwnerのみ可＋最後のOwnerは拒否する安全側の検査を入れる）。

## 暫定採用する具体値（変更容易にするため設定値/定数に集約）

| 項目 | 暫定値 | 代替案 | 変更時の影響 |
|---|---|---|---|
| セッション絶対期限 / アイドル | 14日 / 7日 | 短縮（共有端末が多いなら）／長期ログイン維持 | 設定値のみ。既存セッションは期限の再計算で影響 |
| 招待・再設定トークンの有効期間 | 招待72時間・再設定24時間（初期Ownerは24時間） | 運用に合わせて延長 | 設定値のみ |
| パスワード方針 | 10〜128文字＋簡易な弱パスワード拒否 | 長さのみ(NIST)／より厳格 | ポリシー関数とテストのみ |
| Argon2id | m=64MiB, t=3, p=1、同時4 | OWASP最小(19MiB,t=2)でVPSを軽く | 保存形式にパラメータが入るため、将来の再ハッシュ(ログイン時)で移行可能 |
| ログインのレート制限 | (ID,IP)ごと5回/15分、IPごと10回/10分、IDごと30回/15分（超過で15分ロック。存在しないIDも数える） | DBに保存（再起動で消えない）／より厳格 | **メモリ上**のため再起動でリセット・複数台で共有されない。1台運用が前提。必要ならDBへ移す |
| CSRF方式 | Origin検証＋セッション由来トークン（`X-CSRF-Token`） | Double-submit cookie／SameSite=Strictのみ | ミドルウェアのみ |
| 自動ログイン | 招待受諾後は行わない | 受諾後にそのままログイン | ハンドラーのみ |
| 招待・再設定URLの形式 | `<APP_BASE_URL>/invite#token=…`・`<APP_BASE_URL>/reset#token=…`（フラグメント。JSがPOST本文で送る） | クエリ文字列 | 管理UI(P1-04)と合わせる。クエリだとアクセスログ・履歴・Refererに残る |

## 設計レビュー（Opus、2026-11-21）の反映

レビューの指摘と対応（重大は全て採用）。

**重大**
1. *停止中ユーザーが招待URLで再有効化できる* → 停止のトランザクションで、そのユーザーの未使用トークン（`one_time_tokens`）も全て失効する。招待受諾の更新は`status='invited'`、再設定は`status='active'`を条件にする。再有効化（disabled→active）は管理者が行い、新しいリンクの再発行は別操作。
2. *クライアントIPの取得* → 信頼するプロキシのCIDRを設定値（`API_TRUSTED_PROXY_CIDRS`、既定は空）で明示。リモートアドレスが信頼プロキシの場合に限り、**プロキシが付けた右端のX-Forwarded-For**を採用する。それ以外は`RemoteAddr`を使い、ヘッダーは無視（偽装でIP制限を回避させない）。APIはプロキシ経由でしか到達できない前提（Composeは127.0.0.1のみ公開、本番はNginxの背後）。
3. *トークンをURLのクエリに載せない* → フラグメント（`#token=`）を使う。`/invite`・`/reset`の画面（P1-04）には`Referrer-Policy: no-referrer`・`Cache-Control: no-store`を付け、外部リソースを読み込まない。
4. *最後のOwner保護の競合* → 停止・降格は、Owner行を`SELECT … FOR UPDATE`でロックしたトランザクション内で人数と「対象が管理者か」を判定する（P1-03で降格にも適用。本PRの停止APIで先に実装）。

**中**
- *IDロックが標的型DoSになる* → 完全ロックはしない。**(ID, IP)の組**で5回/15分（超過で15分）、**IPごと**10回/10分、**IDごと**は緩め30回/15分。管理者の解除手段は今回は作らず、メモリ保持のため再起動でも解除される（暫定。DECISIONSに記録）。
- *制限が列挙の手段・メモリ消費になる* → 存在しないIDも同じように数える。キーは小文字化と形式検証（2〜63文字、`login_id`形式）後に使い、形式外は固定キーへ丸める。マップの件数に上限を設け、古い順に捨てる。
- *方針違反でトークンが失われる* → パスワードの方針検証とArgon2計算は**トランザクションの前**に行う。その後に、トークン確保・パスワード保存・状態変更・セッション失効・監査を1トランザクションで行う。
- *invitedの時間差* → 資格情報が無い（存在しない/invited/停止）場合も常にダミーのArgon2id検証を実行する。
- *Argon2の待ち行列* → 同時実行制限は**レート制限の後ろ**に置き、待ちに上限（タイムアウトで503）を設ける。
- *Origin/Refererが無い* → 状態変更で`Origin`も`Referer`も無い、または`Origin: null`は拒否（403）。`Sec-Fetch-Site`が`cross-site`なら拒否を併用。
- *Secureを外す設定* → `APP_BASE_URL`がhttpsなのに`Secure`が無効なら**起動に失敗**する。Secure有効時のCookie名は`__Host-nu_session`（ローカルHTTPのみ`nu_session`）。
- *「パスワード変更」* → 本人によるパスワード変更はAPI.mdに無く本Phaseの範囲外。変更は管理者の再設定フローで行う。

**軽微（採用）**: ログイン時に送られてきた旧セッションCookieも失効、ログアウトはCookieを`Max-Age=0`で削除、`last_seen_at`は1分以上空けて更新、再設定成功で当該IDの失敗カウントも消去、トークンの同時再発行の衝突は409。CSRFトークンはSHA-256のまま（セッションtokenが高エントロピーのため。将来サーバー鍵のHMACへ変えられる）。アイドル7日は運用判断として暫定のまま。

## 実装の分割（積み重ねDraft PR）

- P1-02b: パスワード・セッション・トークンのサービス層（実DBテスト）
- P1-02c: HTTP基盤（ミドルウェア・CSRF・エラー形式・レート制限）とログイン/ログアウト/me
- P1-02d: 招待受諾・再設定
- P1-02e: 管理API（ユーザー作成・リンク発行・停止）と全体のE2E（Compose smoke）

## 参照

OWASP ASVS V2/V3、OWASP Password Storage Cheat Sheet、OWASP CSRF Prevention Cheat Sheet、NIST SP 800-63B。具体的な数値は暫定であり、これらの出典の最小要件を満たす範囲で運用に合わせて調整する。
