//go:build integration

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nu-digital/nu_app/services/api/internal/media"
	"github.com/nu-digital/nu_app/services/api/internal/testutil/pgtest"
)

type mediaFixture struct {
	t    *testing.T
	dsn  string
	dir  string
	st   *media.Store
	exec func(sql string, args ...any)
	n    func(sql string, args ...any) int
	user string
}

func newMediaFixture(t *testing.T) *mediaFixture {
	pool, dsn := pgtest.New(t)
	dir := t.TempDir()
	st, err := media.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &mediaFixture{t: t, dsn: dsn, dir: dir, st: st}
	f.exec = func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	f.n = func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	f.exec(`INSERT INTO users (login_id, status) VALUES ('uploader.example','invited')`)
	return f
}

// add は画像の行とファイルを作る。ageHoursだけ前にアップロードされたことにする。
func (f *mediaFixture) add(id string, ageHours int, refs bool) string {
	f.t.Helper()
	key, err := media.NewKey(id, "png")
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.st.Put(key, []byte("png-bytes-"+id)); err != nil {
		f.t.Fatal(err)
	}
	f.exec(`INSERT INTO media_assets (id, public_id, storage_key, mime_type, byte_size, width, height, sha256, purpose, uploaded_by, created_at)
		VALUES ($1, substr(md5($4::text) || md5($4::text), 1, 22), $2, 'image/png', 20, 10, 10, repeat('a', 64), 'cms', (SELECT id FROM users LIMIT 1), now() - make_interval(hours => $3))`, id, key, ageHours, id)
	if refs {
		f.exec(`INSERT INTO media_references (media_id, owner_type, owner_id, purpose) VALUES ($1, 'content_revision', gen_random_uuid(), 'inline')`, id)
	}
	return key
}

func (f *mediaFixture) cli(args ...string) (int, string, string) {
	f.t.Helper()
	var out, errb bytes.Buffer
	env := map[string]string{"DATABASE_URL": f.dsn, "MEDIA_DIR": f.dir, "NUCTL_OS_USER": "operator"}
	code := run(context.Background(), args, func(k string) string { return env[k] }, &out, &errb)
	return code, out.String(), errb.String()
}

func exists(dir, key string) bool {
	_, err := os.Stat(filepath.Join(dir, key))
	return err == nil
}

const (
	idOldOrphan = "00000000-0000-0000-0000-00000000000a"
	idNewOrphan = "00000000-0000-0000-0000-00000000000b"
	idRefOld    = "00000000-0000-0000-0000-00000000000c"
)

func TestMediaGC(t *testing.T) {
	f := newMediaFixture(t)
	kOld := f.add(idOldOrphan, 48, false)
	kNew := f.add(idNewOrphan, 1, false)
	kRef := f.add(idRefOld, 72, true)
	// 24時間より短い設定は拒否（アップロード直後で参照を付ける前の画像を消さない）
	if code, _, _ := f.cli("media-gc", "--older-than", "30m", "--apply"); code != 2 {
		t.Errorf("30分: %d", code)
	}
	// dry-runは何も消さない
	code, out, _ := f.cli("media-gc")
	if code != 0 || !strings.Contains(out, "dry-run") || !strings.Contains(out, "1 件") {
		t.Fatalf("dry-run: %d %q", code, out)
	}
	if f.n(`SELECT count(*) FROM media_assets`) != 3 || !exists(f.dir, kOld) {
		t.Fatal("dry-runで消えた")
	}
	// --applyは、参照が無く24時間以上前のものだけ（参照のある古い画像・新しい孤児は残る）
	if code, out, e := f.cli("media-gc", "--apply"); code != 0 || !strings.Contains(out, "孤児 1 件") {
		t.Fatalf("apply: %d %q %q", code, out, e)
	}
	if exists(f.dir, kOld) || f.n(`SELECT count(*) FROM media_assets WHERE id=$1`, idOldOrphan) != 0 {
		t.Error("古い孤児が残っている")
	}
	if !exists(f.dir, kNew) || !exists(f.dir, kRef) || f.n(`SELECT count(*) FROM media_assets`) != 2 {
		t.Error("消してはいけないものが消えた")
	}
	// 2回目は何も消さない
	if _, out, _ := f.cli("media-gc", "--apply"); !strings.Contains(out, "孤児 0 件") {
		t.Errorf("2回目: %q", out)
	}
	// 古いtmpファイルも掃除される
	old := filepath.Join(f.dir, "tmp", "stale")
	os.WriteFile(old, []byte("x"), 0o640)
	os.Chtimes(old, timeAgo(48), timeAgo(48))
	if _, out, _ := f.cli("media-gc", "--apply"); !strings.Contains(out, "tmpファイル 1 件") {
		t.Errorf("tmp: %q", out)
	}
	// 設定が足りない
	var out2, err2 bytes.Buffer
	if code := run(context.Background(), []string{"media-gc"}, func(string) string { return "" }, &out2, &err2); code != 2 {
		t.Errorf("環境変数なし: %d", code)
	}
}

func TestMediaVerify(t *testing.T) {
	f := newMediaFixture(t)
	kA := f.add(idOldOrphan, 1, true)
	f.add(idNewOrphan, 1, true)
	if code, out, _ := f.cli("media-verify"); code != 0 || !strings.Contains(out, "食い違いはありません") || !strings.Contains(out, "有効な画像: 2 件") {
		t.Fatalf("正常: %d %q", code, out)
	}
	// ファイルが無い・行が無いファイル・想定外のファイル・無効化済みなのにファイルがある
	os.Remove(filepath.Join(f.dir, kA))
	os.MkdirAll(filepath.Join(f.dir, "ab"), 0o750)
	os.WriteFile(filepath.Join(f.dir, "ab", "abababab-abab-abab-abab-abababababab.png"), []byte("x"), 0o640)
	os.WriteFile(filepath.Join(f.dir, "readme.txt"), []byte("x"), 0o640)
	f.exec(`UPDATE media_assets SET redacted_at=now() WHERE id=$1`, idNewOrphan)
	code, out, _ := f.cli("media-verify")
	for _, want := range []string{"ファイルが無い: id=" + idOldOrphan, "行が無いファイル: ab/abababab", "想定外のファイル", "readme.txt", "無効化済みなのにファイルが残っている: id=" + idNewOrphan, "何も削除していません"} {
		if !strings.Contains(out, want) {
			t.Errorf("報告に %q が無い:\n%s", want, out)
		}
	}
	if code != 1 {
		t.Errorf("食い違いがあるときの終了コード: %d", code)
	}
	// verifyは何も消さない
	if !exists(f.dir, "ab/abababab-abab-abab-abab-abababababab.png") || !exists(f.dir, "readme.txt") {
		t.Error("verifyが消した")
	}
}

func TestMediaRedact(t *testing.T) {
	f := newMediaFixture(t)
	key := f.add(idRefOld, 5, true)
	if code, _, _ := f.cli("media-redact", "--id", idRefOld); code != 2 {
		t.Errorf("理由なし: %d", code)
	}
	if code, _, _ := f.cli("media-redact", "--id", "not-a-uuid", "--reason", "x"); code != 2 {
		t.Errorf("不正なid: %d", code)
	}
	if code, _, _ := f.cli("media-redact", "--id", "00000000-0000-0000-0000-0000000000ff", "--reason", "x"); code != 3 {
		t.Errorf("存在しない: %d", code)
	}
	if !exists(f.dir, key) {
		t.Fatal("失敗した実行でファイルが消えた")
	}
	code, out, e := f.cli("media-redact", "--id", idRefOld, "--reason", "学生証が写り込んでいたため（架空）")
	if code != 0 || !strings.Contains(out, "無効化しました") {
		t.Fatalf("redact: %d %q %q", code, out, e)
	}
	if exists(f.dir, key) {
		t.Error("ファイルが残っている")
	}
	if f.n(`SELECT count(*) FROM media_assets WHERE id=$1 AND redacted_at IS NOT NULL`, idRefOld) != 1 || f.n(`SELECT count(*) FROM media_references WHERE media_id=$1`, idRefOld) != 1 {
		t.Error("行と参照は残す")
	}
	if f.n(`SELECT count(*) FROM audit_logs WHERE action='media.redacted' AND target_id=$1 AND summary_json->>'executedByOsUser'='operator' AND strpos(summary_json::text, '学生証')>0`, idRefOld) != 1 {
		t.Error("監査ログが無い")
	}
	// 二重の実行は拒否され、監査ログも増えない
	if code, _, _ := f.cli("media-redact", "--id", idRefOld, "--reason", "again"); code != 3 || f.n(`SELECT count(*) FROM audit_logs WHERE action='media.redacted'`) != 1 {
		t.Error("二重の実行")
	}
	// verifyで食い違いなし（無効化済みでファイルも無い）
	if code, out, _ := f.cli("media-verify"); code != 0 {
		t.Errorf("verify: %d %s", code, out)
	}
}

func timeAgo(hours int) time.Time { return time.Now().Add(-time.Duration(hours) * time.Hour) }
