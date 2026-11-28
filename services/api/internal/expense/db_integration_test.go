//go:build integration

package expense_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

// 会計テーブルのDB制約: 金額の整合・申請の必須項目・履歴の追記のみ・申請の削除不可。
func TestExpenseSchemaConstraints(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	var user, year string
	must(t, pool.QueryRow(ctx, `INSERT INTO users (login_id, status, activated_at) VALUES ('tester','active',now()) RETURNING id`).Scan(&user))
	must(t, pool.QueryRow(ctx, `INSERT INTO fiscal_years (label, starts_on, ends_on, budget_yen) VALUES ('2026年度','2026-04-01','2027-03-31',900000) RETURNING id`).Scan(&year))
	var cat string
	must(t, pool.QueryRow(ctx, `SELECT id FROM expense_categories WHERE name='物品'`).Scan(&cat))

	insert := func(q string, args ...any) error { _, err := pool.Exec(ctx, q, args...); return err }
	base := `INSERT INTO expense_requests (fiscal_year_id, requester_id, category_id, status, title, reason, receipt_confirmed, quantity, unit_price_yen, shipping_yen, discount_yen, planned_total_yen) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`

	// 合計の不一致は拒否、一致は受理
	if err := insert(base, year, user, cat, "draft", "", "", false, 2, 1000, 500, 0, 9999); err == nil {
		t.Error("合計が合わないのに受理された")
	}
	if err := insert(base, year, user, cat, "draft", "", "", false, 2, 1000, 500, 0, 2500); err != nil {
		t.Errorf("正しい下書きが拒否: %v", err)
	}
	// 申請以降は、件名・理由・領収書発行の確認が必須
	if err := insert(base, year, user, cat, "submitted", "物", "", true, 1, 100, 0, 0, 100); err == nil {
		t.Error("理由なしで申請できた")
	}
	if err := insert(base, year, user, cat, "submitted", "物", "理由", false, 1, 100, 0, 0, 100); err == nil {
		t.Error("領収書の確認なしで申請できた")
	}
	if err := insert(base, year, user, cat, "submitted", "物", "理由", true, 1, 100, 0, 0, 100); err != nil {
		t.Errorf("正しい申請が拒否: %v", err)
	}
	// 申請は削除できない。履歴は追記のみ
	var id string
	must(t, pool.QueryRow(ctx, `SELECT id FROM expense_requests WHERE status='submitted'`).Scan(&id))
	if err := insert(`DELETE FROM expense_requests WHERE id=$1`, id); err == nil || !strings.Contains(err.Error(), "変更・削除できません") {
		t.Errorf("申請を削除できた: %v", err)
	}
	must(t, insert(`INSERT INTO expense_history (request_id, actor_id, action, to_status) VALUES ($1,$2,'submit','submitted')`, id, user))
	if err := insert(`UPDATE expense_history SET note='改ざん'`); err == nil {
		t.Error("履歴を更新できた")
	}
	if err := insert(`DELETE FROM expense_history`); err == nil {
		t.Error("履歴を削除できた")
	}
	// 既定の規則（大学規則として把握済みの4件）
	var n int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM expense_rules WHERE active`).Scan(&n))
	if n != 4 {
		t.Errorf("既定の規則: %d", n)
	}
	_ = pgxpool.Pool{}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
