//go:build integration

package legacyimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

const publicDir = "../../../../apps/web/public"

type fixture struct {
	t    *testing.T
	pool *pgxpool.Pool
}

func newFixture(t *testing.T) *fixture {
	pool, _ := pgtest.New(t)
	f := &fixture{t: t, pool: pool}
	f.exec(`INSERT INTO users (login_id, status, activated_at) VALUES ('owner.example', 'active', now())`)
	f.exec(`INSERT INTO user_roles (user_id, role) SELECT id, 'owner' FROM users WHERE login_id='owner.example'`)
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) n(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func opts(apply bool) Options {
	return Options{ContentDir: contentDir, BaselineDir: baselineDir, AsLoginID: "owner.example", OSUser: "operator", Apply: apply}
}

func (f *fixture) run(o Options) Report {
	f.t.Helper()
	rep, err := Run(context.Background(), f.pool, o)
	if err != nil {
		f.t.Fatalf("Run: %v", err)
	}
	return rep
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(filepath.Join(dst, rel))
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func counts(t *testing.T) (blog, news, project int) {
	arts, _ := Load(contentDir)
	for _, a := range arts {
		switch a.Collection {
		case "blog":
			blog++
		case "news":
			news++
		default:
			project++
		}
	}
	return
}

func TestDryRunWritesNothingAndApplyImportsAll67(t *testing.T) {
	f := newFixture(t)
	rep := f.run(opts(false))
	if len(rep.Problems) != 0 || len(rep.Created) != 67 || rep.Applied {
		t.Fatalf("dry-run: %+v", rep)
	}
	for _, tbl := range []string{"contents", "content_revisions", "content_publication_events", "legacy_authors"} {
		if n := f.n(`SELECT count(*) FROM ` + tbl); n != 0 {
			t.Errorf("dry-runで%sに%d行が残った", tbl, n)
		}
	}
	if n := f.n(`SELECT count(*) FROM audit_logs WHERE action='content.imported'`); n != 0 {
		t.Errorf("dry-runで監査ログが残った")
	}
	rep = f.run(opts(true))
	if len(rep.Problems) != 0 || len(rep.Created) != 67 || !rep.Applied {
		t.Fatalf("apply: %+v", rep)
	}
	blog, news, project := counts(t)
	if f.n(`SELECT count(*) FROM contents WHERE section='blog'`) != blog || f.n(`SELECT count(*) FROM contents WHERE section='news'`) != news || f.n(`SELECT count(*) FROM contents WHERE section='project' AND type='project'`) != project {
		t.Errorf("区分別の件数が違う（blog %d news %d project %d）", blog, news, project)
	}
	// すべて公開済み・版1・公開履歴・監査ログ（sourceIdと実行したOSユーザーだけ）・**通知は積まれない**
	if f.n(`SELECT count(*) FROM contents WHERE published_revision_id IS NULL OR published_revision_id <> working_revision_id`) != 0 {
		t.Error("公開済みでない記事がある")
	}
	if f.n(`SELECT count(*) FROM content_revisions WHERE revision_no <> 1`) != 0 || f.n(`SELECT count(*) FROM content_revisions`) != 67 {
		t.Error("版は各記事1つのはず")
	}
	if f.n(`SELECT count(*) FROM content_publication_events WHERE event_type='publish'`) != 67 {
		t.Error("公開履歴")
	}
	if f.n(`SELECT count(*) FROM audit_logs WHERE action='content.imported' AND summary_json->>'executedByOsUser'='operator' AND summary_json ? 'sourceId' AND NOT (summary_json ? 'author')`) != 67 {
		t.Error("監査ログ")
	}
	if f.n(`SELECT count(*) FROM notification_outbox`) != 0 {
		t.Error("移行で通知（outbox）が積まれた（67件のDiscord通知になる）")
	}
	if f.n(`SELECT count(DISTINCT metadata_json->>'sourceId') FROM content_revisions`) != 67 {
		t.Error("sourceIdが一意でない")
	}
	// 日付: 原文を保ち、kind・sort_date・estimatedが基準（articleDateOrder）と一致
	for _, g := range readGolden[dateGolden](t, "dates.json") {
		var label, kind string
		var sortMs *int64
		var est bool
		err := f.pool.QueryRow(context.Background(), `SELECT r.date_label, r.date_kind, (extract(epoch FROM r.sort_date) * 1000)::bigint, r.sort_date_is_estimated
			FROM contents c JOIN content_revisions r ON r.id=c.working_revision_id WHERE c.section=$1 AND c.slug=$2`, map[string]string{"blog": "blog", "news": "news", "project": "project"}[g.Collection], g.Slug).Scan(&label, &kind, &sortMs, &est)
		if err != nil {
			t.Fatalf("%s: %v", g.Slug, err)
		}
		ms := int64(0)
		if sortMs != nil {
			ms = *sortMs
		}
		if label != g.Label || ms != g.Order || (kind == "day") == est {
			t.Errorf("%s: label=%q kind=%s sort=%d est=%v（基準 %q %d）", g.Slug, label, kind, ms, est, g.Label, g.Order)
		}
	}
	// 本文: コメントを除いて移行元と一致（コメントの無い記事は完全に同じ）。空の抜粋は空のまま
	inv := map[string]string{}
	var invDoc struct {
		Articles []struct{ Collection, Slug, BodySha256 string }
	}
	b, _ := os.ReadFile(filepath.Join(baselineDir, "inventory.json"))
	_ = json.Unmarshal(b, &invDoc)
	for _, a := range invDoc.Articles {
		inv[a.Collection+"/"+a.Slug] = a.BodySha256
	}
	arts, _ := Load(contentDir)
	var plain, empty int
	for _, a := range arts {
		var md, summary, meta string
		if err := f.pool.QueryRow(context.Background(), `SELECT r.markdown, r.summary, r.metadata_json::text FROM contents c JOIN content_revisions r ON r.id=c.working_revision_id WHERE c.slug=$1 AND c.section=$2`, a.Slug, a.Collection).Scan(&md, &summary, &meta); err != nil {
			t.Fatalf("%s: %v", a.Slug, err)
		}
		if _, url := ExtractSourceComment(a.Body); url == "" {
			sum := sha256.Sum256([]byte(md))
			if hex.EncodeToString(sum[:]) != inv[a.Collection+"/"+a.Slug] {
				t.Errorf("%s: コメントの無い本文が基準（bodySha256）と違う", a.Slug)
			}
			plain++
		}
		if a.Excerpt == "" {
			empty++
			if summary != "" || strings.Contains(meta, `"excerpt"`) {
				t.Errorf("%s: 空の抜粋が保たれていない", a.Slug)
			}
		}
	}
	if plain != 44 || empty != 11 {
		t.Errorf("コメント無し %d・空の抜粋 %d", plain, empty)
	}
}

// D14: 投稿者はlegacy_authorsで管理する。名前は版・メタデータに保存しない。同じ投稿者の記事は同じ行を共有。公開可否は未確認から。
func TestAuthorsAreManagedSeparatelyAndStartUnconfirmed(t *testing.T) {
	f := newFixture(t)
	rep := f.run(opts(true))
	if rep.AuthorsBefore != 0 || rep.AuthorsAfter != 3 {
		t.Fatalf("投稿者 %d → %d（yurarara・iwa・mizmelonの3件のはず）", rep.AuthorsBefore, rep.AuthorsAfter)
	}
	if f.n(`SELECT count(*) FROM legacy_authors WHERE publication_state='unconfirmed' AND confirmed_by IS NULL AND linked_user_id IS NULL AND erased_at IS NULL`) != 3 {
		t.Error("移行直後は、すべて未確認・未紐付けのはず")
	}
	// 団体名（nu_digital）は投稿者なし（NULL）。それ以外は投稿者を参照し、同じ投稿者の記事は同じ行を共有する
	if f.n(`SELECT count(*) FROM contents WHERE legacy_author_id IS NULL`) != 7 || f.n(`SELECT count(*) FROM contents WHERE legacy_author_id IS NOT NULL`) != 60 {
		t.Error("投稿者の有無の内訳")
	}
	if f.n(`SELECT count(*) FROM contents c JOIN legacy_authors a ON a.id=c.legacy_author_id WHERE a.source_name='yurarara'`) != 58 {
		t.Error("同じ投稿者の記事が同じ行を共有していない")
	}
	// 名前は版・メタデータに入っていない
	for _, name := range []string{"yurarara", "iwa", "mizmelon"} {
		// メタデータに、投稿者名を値として持つ項目が無い（タイトル・本文に語として出てくることは別）
		if f.n(`SELECT count(*) FROM content_revisions r, jsonb_each_text(r.metadata_json) kv WHERE kv.value = $1`, name) != 0 {
			t.Errorf("版のメタデータに投稿者名 %s が値として入っている（D14: 版に保存しない）", name)
		}
	}
	if f.n(`SELECT count(*) FROM content_revisions WHERE metadata_json ? 'author'`) != 0 {
		t.Error("メタデータにauthorキーがある")
	}
	// 公開許可の記録が無いまま「許可」にはできない（DBの制約）
	if _, err := f.pool.Exec(context.Background(), `UPDATE legacy_authors SET publication_state='permitted' WHERE source_name='yurarara'`); err == nil {
		t.Error("確認者・日時が無いまま許可にできた")
	}
	// 再実行しても、状態（許可・紐付け）は変わらない
	f.exec(`UPDATE legacy_authors SET publication_state='permitted', confirmed_by=(SELECT id FROM users LIMIT 1), confirmed_at=now() WHERE source_name='iwa'`)
	f.exec(`UPDATE legacy_authors SET linked_user_id=(SELECT id FROM users LIMIT 1), linked_at=now() WHERE source_name='mizmelon'`)
	rep = f.run(opts(true))
	if len(rep.Created) != 0 || len(rep.Skipped) != 67 || rep.AuthorsAfter != 3 {
		t.Fatalf("再実行: %+v", rep)
	}
	if f.n(`SELECT count(*) FROM legacy_authors WHERE source_name='iwa' AND publication_state='permitted'`) != 1 || f.n(`SELECT count(*) FROM legacy_authors WHERE source_name='mizmelon' AND linked_user_id IS NOT NULL AND publication_state='unconfirmed'`) != 1 {
		t.Error("再実行で投稿者の状態が変わった")
	}
}

func TestRerunIsIdempotentAndNeverTouchesPublishState(t *testing.T) {
	f := newFixture(t)
	f.run(opts(true))
	before := f.n(`SELECT count(*) FROM content_revisions`)
	rep := f.run(opts(true))
	if len(rep.Created) != 0 || len(rep.Skipped) != 67 || len(rep.Drift) != 0 || len(rep.Problems) != 0 {
		t.Fatalf("再実行: %+v", rep)
	}
	if f.n(`SELECT count(*) FROM content_revisions`) != before || f.n(`SELECT count(*) FROM contents`) != 67 || f.n(`SELECT count(*) FROM content_publication_events`) != 67 || f.n(`SELECT count(*) FROM audit_logs WHERE action='content.imported'`) != 67 {
		t.Error("再実行で行が増えた")
	}
	// CMSで公開停止した記事は、再実行しても再公開されない（作業版は移行元と同じなのでスキップ扱い）
	f.exec(`UPDATE contents SET published_revision_id=NULL WHERE slug='nu-bar'`)
	rep = f.run(opts(true))
	if len(rep.Created) != 0 || f.n(`SELECT count(*) FROM contents WHERE slug='nu-bar' AND published_revision_id IS NULL`) != 1 {
		t.Errorf("公開停止中の記事が再公開された: %+v", rep)
	}
	if f.n(`SELECT count(*) FROM content_publication_events`) != 67 {
		t.Error("再実行で公開履歴が増えた")
	}
	// CMSで編集された記事（作業版が移行元と違う）は、上書きせず「差分あり」として報告する
	f.exec(`INSERT INTO content_revisions (content_id, revision_no, title, markdown, edited_by, metadata_json) SELECT c.id, 2, 'CMSで直したタイトル', 'CMSで直した本文', c.creator_user_id, r.metadata_json FROM contents c JOIN content_revisions r ON r.id=c.working_revision_id WHERE c.slug='demo-yura'`)
	f.exec(`UPDATE contents SET working_revision_id=(SELECT id FROM content_revisions WHERE title='CMSで直したタイトル') WHERE slug='demo-yura'`)
	rep = f.run(opts(true))
	if len(rep.Drift) != 1 || !strings.Contains(rep.Drift[0], "project/demo-yura") || len(rep.Created) != 0 {
		t.Fatalf("差分の報告: %+v", rep)
	}
	var title string
	_ = f.pool.QueryRow(context.Background(), `SELECT r.title FROM contents c JOIN content_revisions r ON r.id=c.working_revision_id WHERE c.slug='demo-yura'`).Scan(&title)
	if title != "CMSで直したタイトル" || f.n(`SELECT count(*) FROM content_revisions WHERE content_id=(SELECT id FROM contents WHERE slug='demo-yura')`) != 2 {
		t.Error("CMSでの編集を上書きした")
	}
}

func TestConflictingSlugAborts(t *testing.T) {
	f := newFixture(t)
	// 同じ区分・slugで、別のsourceIdの記事が既にある → 衝突として全体を中止（何も書かない）
	f.insertForeign("blog", "activity-2025-12-22-6db7c7", "blog/other-source")
	before := f.n(`SELECT count(*) FROM contents`)
	rep := f.run(opts(true))
	if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "衝突") || rep.Applied {
		t.Fatalf("衝突: %+v", rep)
	}
	if f.n(`SELECT count(*) FROM contents`) != before || f.n(`SELECT count(*) FROM legacy_authors`) != 0 {
		t.Error("衝突なのに他の記事・投稿者が書かれた")
	}
}

// 別のsourceIdの記事を直接入れる（衝突の再現用）
func (f *fixture) insertForeign(section, slug, sourceID string) {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	run := func(sql string, args ...any) {
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			f.t.Fatal(err)
		}
	}
	run(`SET CONSTRAINTS contents_working_revision_fk, contents_published_revision_fk, content_revisions_content_id_fkey DEFERRED`)
	var cid, rid string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&cid, &rid); err != nil {
		f.t.Fatal(err)
	}
	run(`INSERT INTO contents (id, type, section, slug, creator_user_id, working_revision_id) VALUES ($1::uuid, 'article', $2, $3, (SELECT id FROM users LIMIT 1), $4::uuid)`, cid, section, slug, rid)
	run(`INSERT INTO content_revisions (id, content_id, revision_no, title, markdown, edited_by, metadata_json) VALUES ($1::uuid, $2::uuid, 1, '別の記事', 'x', (SELECT id FROM users LIMIT 1), jsonb_build_object('sourceId', $3::text))`, rid, cid, sourceID)
	if err := tx.Commit(ctx); err != nil {
		f.t.Fatal(err)
	}
}

func TestInputViolationsWriteNothing(t *testing.T) {
	f := newFixture(t)
	tmp := t.TempDir()
	copyDir(t, contentDir, filepath.Join(tmp, "content"))
	copyDir(t, baselineDir, filepath.Join(tmp, "baseline"))
	bad := filepath.Join(tmp, "content", "blog", "activity-2025-12-22-6db7c7.md")
	b, _ := os.ReadFile(bad)
	if err := os.WriteFile(bad, append(b, []byte("\n<script>alert(1)</script>\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	o := Options{ContentDir: filepath.Join(tmp, "content"), BaselineDir: filepath.Join(tmp, "baseline"), AsLoginID: "owner.example", Apply: true}
	rep := f.run(o)
	if len(rep.Problems) == 0 || rep.Applied {
		t.Fatalf("違反があるのに通った: %+v", rep)
	}
	var sha, script bool
	for _, p := range rep.Problems {
		sha = sha || strings.Contains(p, "inventory.json）と違います")
		script = script || strings.Contains(p, "script")
	}
	if !sha || !script {
		t.Errorf("sha256の不一致と本文の違反の両方が報告されるはず: %v", rep.Problems)
	}
	if f.n(`SELECT count(*) FROM contents`) != 0 || f.n(`SELECT count(*) FROM legacy_authors`) != 0 {
		t.Error("違反があるのに他の記事が書かれた")
	}
	// ソースの変更を意図して許可しても、本文の違反は拒否される（移行だけが緩くならない）
	o.AllowSourceChanges = true
	if rep := f.run(o); len(rep.Problems) == 0 || f.n(`SELECT count(*) FROM contents`) != 0 {
		t.Errorf("--allow-source-changesで本文の違反まで通った: %+v", rep.Problems)
	}
	// frontmatterの違反（category）
	os.WriteFile(bad, b, 0o644)
	nb := strings.Replace(string(b), "category: \"Blog\"", "category: \"Other\"", 1)
	os.WriteFile(bad, []byte(nb), 0o644)
	o.AllowSourceChanges = true
	if rep := f.run(o); len(rep.Problems) == 0 {
		t.Error("categoryの違反が通った")
	}
}

func TestOwnerIsVerifiedInsideTheTransaction(t *testing.T) {
	f := newFixture(t)
	f.exec(`INSERT INTO users (login_id, status, activated_at) VALUES ('member.example', 'active', now())`)
	f.exec(`INSERT INTO users (login_id, status, activated_at) VALUES ('admin.example', 'active', now())`)
	f.exec(`INSERT INTO user_roles (user_id, role) SELECT id, 'admin' FROM users WHERE login_id='admin.example'`)
	f.exec(`INSERT INTO users (login_id, status, disabled_at) VALUES ('gone.example', 'disabled', now())`)
	f.exec(`INSERT INTO user_roles (user_id, role) SELECT id, 'owner' FROM users WHERE login_id='gone.example'`)
	for _, who := range []string{"ghost.example", "member.example", "admin.example", "gone.example"} {
		o := opts(true)
		o.AsLoginID = who
		rep := f.run(o)
		if len(rep.Problems) != 1 || rep.Applied || f.n(`SELECT count(*) FROM contents`) != 0 {
			t.Errorf("--as %s（Ownerでない・無効）が通った: %+v", who, rep)
		}
	}
	if rep := f.run(opts(true)); !rep.Applied {
		t.Fatalf("Ownerでの実行: %+v", rep)
	}
}

func TestTakedownArticlesAreNotImported(t *testing.T) {
	f := newFixture(t)
	tmp := t.TempDir()
	copyDir(t, baselineDir, filepath.Join(tmp, "baseline"))
	td, _ := json.Marshal(map[string]any{"articles": []map[string]string{{"section": "blog", "slug": "activity-2025-12-22-6db7c7", "reason": "試験", "date": "2026-11-21"}}, "images": []any{}})
	os.WriteFile(filepath.Join(tmp, "baseline", "takedown.json"), td, 0o644)
	o := opts(true)
	o.BaselineDir = filepath.Join(tmp, "baseline")
	rep := f.run(o)
	if len(rep.Takedown) != 1 || len(rep.Created) != 66 || f.n(`SELECT count(*) FROM contents WHERE slug='activity-2025-12-22-6db7c7'`) != 0 {
		t.Fatalf("取り下げ一覧: %+v", rep)
	}
	// 再実行（災害復旧後など）でも、取り下げた記事は入らない
	if rep := f.run(o); len(rep.Created) != 0 || f.n(`SELECT count(*) FROM contents`) != 66 {
		t.Errorf("再実行で取り下げた記事が入った: %+v", rep)
	}
}

func TestImageExistenceCheck(t *testing.T) {
	f := newFixture(t)
	o := opts(false)
	o.PublicDir = publicDir
	if rep := f.run(o); len(rep.Problems) != 0 {
		t.Fatalf("実在する画像で問題: %v", rep.Problems[:min(3, len(rep.Problems))])
	}
	o.PublicDir = t.TempDir() // 画像が1つも無い
	rep := f.run(o)
	if len(rep.Problems) == 0 || !strings.Contains(rep.Problems[0], "画像が見つかりません") {
		t.Errorf("画像の欠けを検出しない: %+v", rep.Problems)
	}
}

func TestConcurrentImportsDoNotDuplicate(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	reps := make([]Report, 4)
	errs := make([]error, 4)
	for i := range reps {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reps[i], errs[i] = Run(context.Background(), f.pool, opts(true))
		}(i)
	}
	wg.Wait()
	created := 0
	for i, r := range reps {
		if errs[i] != nil || len(r.Problems) != 0 {
			t.Errorf("同時実行%d: %v %v", i, errs[i], r.Problems)
		}
		created += len(r.Created)
	}
	if created != 67 || f.n(`SELECT count(*) FROM contents`) != 67 || f.n(`SELECT count(*) FROM legacy_authors`) != 3 || f.n(`SELECT count(*) FROM content_publication_events`) != 67 {
		t.Errorf("同時実行で重複・欠落: created=%d contents=%d", created, f.n(`SELECT count(*) FROM contents`))
	}
}
