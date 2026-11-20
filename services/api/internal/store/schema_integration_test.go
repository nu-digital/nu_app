//go:build integration

package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/store"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

// 以下のログインID・表示名などはすべて架空の値。

func mustUser(t *testing.T, pool *pgxpool.Pool, loginID, status string) store.User {
	t.Helper()
	u, err := store.New(pool).CreateUser(context.Background(), store.CreateUserParams{LoginID: loginID, Status: status})
	if err != nil {
		t.Fatalf("ユーザー作成に失敗: %v", err)
	}
	return u
}

// wantViolation は SQL が指定した制約名（またはSQLSTATE）で拒否されることを確認する。
func wantViolation(t *testing.T, pool *pgxpool.Pool, constraint, sql string, args ...any) {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		t.Fatalf("%s: 拒否されるべきSQLが通った（err=%v）", constraint, err)
	}
	if pe.ConstraintName != constraint && pe.Code != constraint {
		t.Fatalf("期待した違反 %q と異なる: constraint=%q code=%q msg=%s", constraint, pe.ConstraintName, pe.Code, pe.Message)
	}
}

func TestMigrationsAreRepeatableAndReversible(t *testing.T) {
	_, dsn := pgtest.New(t)
	pgtest.Migrate(t, dsn)          // 2回目のup（適用済み）は何もしない
	pgtest.MigrateDownTo(t, dsn, 0) // 全部戻せる
	pgtest.Migrate(t, dsn)          // 再度upできる
}

func TestUsers(t *testing.T) {
	pool, _ := pgtest.New(t)
	ok := mustUser(t, pool, "alice.example", "invited")
	if ok.Status != "invited" || ok.DisabledAt.Valid {
		t.Fatalf("初期状態が想定と異なる: %+v", ok)
	}
	got, err := store.New(pool).GetUserByLoginID(context.Background(), "alice.example")
	if err != nil || got.ID != ok.ID {
		t.Fatalf("login_idで取得できない: %v", err)
	}
	wantViolation(t, pool, "users_login_id_unique", `INSERT INTO users (login_id) VALUES ('alice.example')`)
	for _, bad := range []string{"", "A", "Alice", "a", "-abc", "has space", "日本語", strings.Repeat("a", 64)} {
		wantViolation(t, pool, "users_login_id_format", `INSERT INTO users (login_id) VALUES ($1)`, bad)
	}
	wantViolation(t, pool, "users_status_valid", `INSERT INTO users (login_id, status) VALUES ('bob.example', 'deleted')`)
	// 停止状態とdisabled_atは一致していなければならない
	wantViolation(t, pool, "users_disabled_at_matches_status", `INSERT INTO users (login_id, status) VALUES ('c.example', 'disabled')`)
	wantViolation(t, pool, "users_disabled_at_matches_status", `INSERT INTO users (login_id, status, disabled_at) VALUES ('d.example', 'active', now())`)
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (login_id, status, disabled_at) VALUES ('e.example', 'disabled', now())`); err != nil {
		t.Fatalf("停止状態の正しい登録が拒否された: %v", err)
	}
}

func TestSessionsAndTokensStoreOnlyHashes(t *testing.T) {
	pool, _ := pgtest.New(t)
	u := mustUser(t, pool, "alice.example", "active")
	hash := make([]byte, 32)
	hash[0] = 1
	exec := func(sql string, args ...any) error {
		_, err := pool.Exec(context.Background(), sql, args...)
		return err
	}

	if err := exec(`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, u.ID, hash); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "sessions_token_hash_unique", `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, u.ID, hash)
	wantViolation(t, pool, "sessions_token_hash_length", `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, u.ID, []byte("plain-session-id"))
	other := make([]byte, 32)
	other[0] = 2
	wantViolation(t, pool, "sessions_expires_after_created", `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, now() - interval '1 hour')`, u.ID, other)

	// one_time_tokens: hash長、期限、purpose、有効なtokenは(user,purpose)ごとに1件
	t1, t2, t3 := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	t1[0], t2[0], t3[0] = 11, 12, 13
	ins := `INSERT INTO one_time_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 day')`
	if err := exec(ins, u.ID, "invite", t1); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "one_time_tokens_one_active_idx", ins, u.ID, "invite", t2)
	if err := exec(ins, u.ID, "password_reset", t2); err != nil {
		t.Fatalf("別purposeのtokenは同時に有効にできるべき: %v", err)
	}
	wantViolation(t, pool, "one_time_tokens_purpose_valid", ins, u.ID, "login", t3)
	wantViolation(t, pool, "one_time_tokens_hash_length", ins, u.ID, "invite", []byte("raw-token"))
	// 失効または使用済みにすれば新しいtokenを発行できる（再発行）
	if err := exec(`UPDATE one_time_tokens SET revoked_at = now() WHERE user_id = $1 AND purpose = 'invite'`, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := exec(ins, u.ID, "invite", t3); err != nil {
		t.Fatalf("失効後の再発行が拒否された: %v", err)
	}
	wantViolation(t, pool, "one_time_tokens_not_used_and_revoked", `UPDATE one_time_tokens SET used_at = now(), revoked_at = now() WHERE token_hash = $1`, t3)
	wantViolation(t, pool, "one_time_tokens_expires_after_created", `INSERT INTO one_time_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1, 'invite', $2, now() - interval '1 day')`, mustUser(t, pool, "bob.example", "invited").ID, make([]byte, 32))
}

func TestRolesAndOwnerTransfer(t *testing.T) {
	pool, _ := pgtest.New(t)
	a := mustUser(t, pool, "alice.example", "active")
	b := mustUser(t, pool, "bob.example", "active")
	c := mustUser(t, pool, "carol.example", "active")
	exec := func(sql string, args ...any) error {
		_, err := pool.Exec(context.Background(), sql, args...)
		return err
	}

	if err := exec(`INSERT INTO user_roles (user_id, role) VALUES ($1, 'owner')`, a.ID); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "user_roles_pkey", `INSERT INTO user_roles (user_id, role) VALUES ($1, 'owner')`, a.ID)
	// Memberは行を持たないので 'member' ロールは登録できない
	wantViolation(t, pool, "user_roles_role_valid", `INSERT INTO user_roles (user_id, role) VALUES ($1, 'member')`, b.ID)

	ins := `INSERT INTO owner_transfer_requests (from_user_id, to_user_id, expires_at) VALUES ($1, $2, now() + interval '24 hours')`
	if err := exec(ins, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "owner_transfer_one_pending_per_owner_idx", ins, a.ID, c.ID)
	wantViolation(t, pool, "owner_transfer_not_self", ins, c.ID, c.ID)
	wantViolation(t, pool, "owner_transfer_expires_after_request", `INSERT INTO owner_transfer_requests (from_user_id, to_user_id, expires_at) VALUES ($1, $2, now() - interval '1 hour')`, b.ID, c.ID)
	// resolved_at は pending 以外のときだけ設定される
	wantViolation(t, pool, "owner_transfer_resolved_matches_status", `UPDATE owner_transfer_requests SET status = 'cancelled' WHERE from_user_id = $1`, a.ID)
	if err := exec(`UPDATE owner_transfer_requests SET status = 'cancelled', resolved_at = now() WHERE from_user_id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := exec(ins, a.ID, c.ID); err != nil {
		t.Fatalf("取消後は新しい申請を出せるべき: %v", err)
	}
}

func TestProfilesAndConsent(t *testing.T) {
	pool, _ := pgtest.New(t)
	u := mustUser(t, pool, "alice.example", "active")
	exec := func(sql string, args ...any) error {
		_, err := pool.Exec(context.Background(), sql, args...)
		return err
	}

	// 表示名のみで登録でき、他の項目はNULLのままでよい（D02）
	if err := exec(`INSERT INTO profiles (user_id, display_name) VALUES ($1, 'アリス(架空)')`, u.ID); err != nil {
		t.Fatalf("表示名だけのプロフィールが拒否された: %v", err)
	}
	v := mustUser(t, pool, "bob.example", "active")
	for _, name := range []string{"", "   ", strings.Repeat("あ", 65)} {
		wantViolation(t, pool, "profiles_display_name_not_blank", `INSERT INTO profiles (user_id, display_name) VALUES ($1, $2)`, v.ID, name)
	}
	wantViolation(t, pool, "profiles_school_year_range", `UPDATE profiles SET school_year = 99 WHERE user_id = $1`, u.ID)
	// 個人情報は全項目任意（空の行でよい）
	if err := exec(`INSERT INTO personal_profiles (user_id) VALUES ($1)`, u.ID); err != nil {
		t.Fatalf("個人情報の行は全項目NULLで作れるべき: %v", err)
	}
	// メンバー内公開の設定: 個人情報4項目は対象外、値はprivate/membersのみ
	for _, key := range []string{"legal_name", "university_email", "student_number", "nearest_station", "display_name"} {
		wantViolation(t, pool, "profile_field_key_valid", `INSERT INTO profile_field_visibility (user_id, field_key, visibility) VALUES ($1, $2, 'members')`, u.ID, key)
	}
	wantViolation(t, pool, "profile_field_visibility_valid", `INSERT INTO profile_field_visibility (user_id, field_key, visibility) VALUES ($1, 'bio', 'public')`, u.ID)
	if err := exec(`INSERT INTO profile_field_visibility (user_id, field_key, visibility) VALUES ($1, 'bio', 'members')`, u.ID); err != nil {
		t.Fatal(err)
	}
	// インターネット公開の同意は別テーブル（D02）。撤回は付与以降の時刻のみ
	if err := exec(`INSERT INTO public_credit_consents (user_id, granted_at) VALUES ($1, now())`, u.ID); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "public_credit_withdrawn_after_granted", `UPDATE public_credit_consents SET withdrawn_at = now() - interval '1 day' WHERE user_id = $1`, u.ID)
}

func TestAppendOnlyTablesAndBootstrapOnce(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	u := mustUser(t, pool, "alice.example", "active")
	if err := store.New(pool).InsertAuditLog(ctx, store.InsertAuditLogParams{ActorUserID: u.ID, Action: "test.action", SummaryJson: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "23001", `UPDATE audit_logs SET action = 'tampered'`)
	wantViolation(t, pool, "23001", `DELETE FROM audit_logs`)
	if _, err := pool.Exec(ctx, `INSERT INTO emergency_recovery_events (executed_by_os_user, target_user_id, approver_note) VALUES ('operator', $1, '本人確認済み(架空)')`, u.ID); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "23001", `UPDATE emergency_recovery_events SET approver_note = 'x'`)
	wantViolation(t, pool, "23001", `DELETE FROM emergency_recovery_events`)
	wantViolation(t, pool, "emergency_recovery_events_approver_note_check", `INSERT INTO emergency_recovery_events (executed_by_os_user, target_user_id, approver_note) VALUES ('operator', $1, '  ')`, u.ID)

	// bootstrap_state は1行だけ
	if _, err := pool.Exec(ctx, `INSERT INTO bootstrap_state DEFAULT VALUES`); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "bootstrap_state_pkey", `INSERT INTO bootstrap_state DEFAULT VALUES`)
	_ = time.Now
}

func TestProfileExtrasConstraints(t *testing.T) {
	pool, _ := pgtest.New(t)
	u := mustUser(t, pool, "alice.example", "active")
	ins := `INSERT INTO profile_links (user_id, label, url, visibility) VALUES ($1, $2, $3, $4)`
	if _, err := pool.Exec(context.Background(), ins, u.ID, "GitHub", "https://github.com/example", "members"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"javascript:alert(1)", "data:text/html,x", "ftp://example.invalid", "/relative", "https://with space", "https://" + strings.Repeat("a", 2100)} {
		wantViolation(t, pool, "profile_links_url_valid", ins, u.ID, "x", bad, "private")
	}
	wantViolation(t, pool, "profile_links_visibility_valid", ins, u.ID, "x", "https://example.invalid", "public")
	wantViolation(t, pool, "profile_links_label_valid", ins, u.ID, "  ", "https://example.invalid", "private")
	dev := `INSERT INTO personal_devices (user_id, category, name, specs_json, visibility) VALUES ($1, $2, $3, $4::jsonb, $5)`
	if _, err := pool.Exec(context.Background(), dev, u.ID, "PC", "自作PC", `{"text":"RTX"}`, "private"); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "personal_devices_specs_object", dev, u.ID, "PC", "x", `[1,2]`, "private")
	wantViolation(t, pool, "personal_devices_specs_object", dev, u.ID, "PC", "x", `{"text":"`+strings.Repeat("a", 3000)+`"}`, "private")
	wantViolation(t, pool, "personal_devices_visibility_valid", dev, u.ID, "PC", "x", `{}`, "everyone")
	wantViolation(t, pool, "personal_devices_name_valid", dev, u.ID, "PC", " ", `{}`, "private")
}

func TestInventoryConstraints(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	u := mustUser(t, pool, "alice.example", "active")
	v := mustUser(t, pool, "bob.example", "active")
	var qty, ind string
	if err := pool.QueryRow(ctx, `INSERT INTO inventory_items (name, mode, total_quantity) VALUES ('ケーブル','quantity',5) RETURNING id::text`).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO inventory_items (name, mode) VALUES ('Pico','individual') RETURNING id::text`).Scan(&ind); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "inventory_items_total_valid", `INSERT INTO inventory_items (name, mode, total_quantity) VALUES ('x','quantity',-1)`)
	wantViolation(t, pool, "inventory_items_total_valid", `INSERT INTO inventory_items (name, mode, total_quantity) VALUES ('x','individual',3)`)
	wantViolation(t, pool, "inventory_items_mode_valid", `INSERT INTO inventory_items (name, mode) VALUES ('x','both')`)
	wantViolation(t, pool, "inventory_items_name_valid", `INSERT INTO inventory_items (name, mode) VALUES ('  ','quantity')`)

	// 個体: 管理番号は物品内で一意。貸出中は廃止できない。1つの貸出が複数個体を占有しない（=個体は同時に1貸出まで）
	var u1, u2 string
	if err := pool.QueryRow(ctx, `INSERT INTO inventory_units (item_id, asset_tag) VALUES ($1,'P1') RETURNING id::text`, ind).Scan(&u1); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO inventory_units (item_id, asset_tag) VALUES ($1,'P2') RETURNING id::text`, ind).Scan(&u2); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "inventory_units_tag_unique", `INSERT INTO inventory_units (item_id, asset_tag) VALUES ($1,'P1')`, ind)
	var l1 string
	if err := pool.QueryRow(ctx, `INSERT INTO inventory_loans (item_id, unit_id, borrower_user_id, quantity) VALUES ($1,$2,$3,1) RETURNING id::text`, ind, u1, u.ID).Scan(&l1); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_units SET current_loan_id=$2 WHERE id=$1`, u1, l1); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "inventory_units_current_loan_unique", `UPDATE inventory_units SET current_loan_id=$2 WHERE id=$1`, u2, l1)
	wantViolation(t, pool, "inventory_units_loaned_not_archived", `UPDATE inventory_units SET state='archived', archived_at=now() WHERE id=$1`, u1)
	// 貸出: 個体の貸出は数量1、数量は1以上
	wantViolation(t, pool, "inventory_loans_unit_quantity", `INSERT INTO inventory_loans (item_id, unit_id, borrower_user_id, quantity) VALUES ($1,$2,$3,2)`, ind, u2, v.ID)
	wantViolation(t, pool, "inventory_loans_quantity_valid", `INSERT INTO inventory_loans (item_id, borrower_user_id, quantity) VALUES ($1,$2,0)`, qty, v.ID)
	wantViolation(t, pool, "inventory_returns_quantity_valid", `INSERT INTO inventory_returns (loan_id, actor_user_id, quantity) VALUES ($1,$2,0)`, l1, v.ID)
	// 元記録は追記専用（更新・削除できない）
	wantViolation(t, pool, "23001", `UPDATE inventory_loans SET quantity = 1`)
	wantViolation(t, pool, "23001", `DELETE FROM inventory_loans`)
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_returns (loan_id, actor_user_id, quantity) VALUES ($1,$2,1)`, l1, v.ID); err != nil {
		t.Fatal(err)
	}
	wantViolation(t, pool, "23001", `UPDATE inventory_returns SET quantity = 1`)
	wantViolation(t, pool, "23001", `DELETE FROM inventory_returns`)
	wantViolation(t, pool, "inventory_adjustments_changed", `INSERT INTO inventory_adjustments (item_id, actor_user_id, before_quantity, after_quantity, reason) VALUES ($1,$2,3,3,'x')`, qty, u.ID)
	wantViolation(t, pool, "inventory_adjustments_reason_valid", `INSERT INTO inventory_adjustments (item_id, actor_user_id, before_quantity, after_quantity, reason) VALUES ($1,$2,3,4,' ')`, qty, u.ID)
}
