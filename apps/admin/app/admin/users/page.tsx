"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";

import {
  createUser, deletePrivateProfile, describe, fetchMe, getPrivateProfile, issueLink, listUsers, setRole, setUserStatus,
  type AdminUser, type LinkResult, type Me, type PrivateProfile,
} from "@/lib/api";

const STATUS_LABEL: Record<AdminUser["status"], string> = { active: "有効", invited: "招待中", disabled: "停止中" };
const ROLE_LABEL: Record<string, string> = { owner: "Owner", admin: "Admin", editor: "Editor" };

type Issued = { title: string; url: string; expiresAt: string };

export default function UsersPage() {
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);
  const [forbidden, setForbidden] = useState(false);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [issued, setIssued] = useState<Issued | null>(null);
  const [confirming, setConfirming] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [priv, setPriv] = useState<PrivateProfile | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [anonymize, setAnonymize] = useState(true);
  const [addOpen, setAddOpen] = useState(false); // スマホで一覧が埋もれないよう、追加フォームは折りたたんでおく
  const noticeRef = useRef<HTMLDivElement>(null);

  const isOwner = me?.user.roles.includes("owner") ?? false;
  const isAdmin = isOwner || (me?.user.roles.includes("admin") ?? false);

  const load = useCallback(async (search: string) => {
    setLoading(true);
    try {
      const r = await listUsers(search);
      setUsers(r.items);
      setNext(r.nextCursor);
      setError("");
    } catch (e) {
      if ((e as { status?: number }).status === 403) setForbidden(true);
      else setError(describe(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    let alive = true;
    fetchMe()
      .then((m) => {
        if (!alive) return;
        if (!m) return router.replace("/login");
        setMe(m);
        void load("");
      })
      .catch((e) => alive && (setError(describe(e)), setLoading(false)));
    return () => {
      alive = false;
    };
  }, [router, load]);

  useEffect(() => {
    if (notice || error) noticeRef.current?.focus();
  }, [notice, error]);

  async function run(id: string, f: () => Promise<void>, done: string) {
    setBusyId(id);
    setError("");
    setNotice("");
    try {
      await f();
      setNotice(done);
      await load(query);
    } catch (e) {
      setError(describe(e));
    } finally {
      setBusyId(null);
      setConfirming(null);
    }
  }

  function showLink(title: string, r: LinkResult) {
    setIssued({ title, url: r.url, expiresAt: r.expiresAt });
  }

  if (forbidden || (me && !isAdmin && !loading && users.length === 0 && !error)) {
    return (
      <main className="auth-wrap">
        <div className="auth-card">
          <h1>ユーザー管理</h1>
          <div className="notice error" role="alert">
            <strong>エラー</strong>
            <span>この画面を使う権限がありません。管理者（Admin・Owner）だけが使えます。</span>
          </div>
          <Link className="btn" href="/">
            ランチャーへ戻る
          </Link>
        </div>
      </main>
    );
  }

  return (
    <>
      <header className="app-header">
        <Link className="brand" href="/">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/images/logo.webp" alt="" width={36} height={36} />
          <span className="brand-text">nu_digital ポータル</span>
        </Link>
        <Link className="btn secondary inline" href="/">
          ランチャーへ
        </Link>
      </header>
      <main className="page">
        <h1>ユーザー管理</h1>

        {(notice || error) && (
          <div className={`notice ${error ? "error" : "success"}`} role={error ? "alert" : "status"} tabIndex={-1} ref={noticeRef}>
            <strong>{error ? "エラー" : "完了"}</strong>
            <span>{error || notice}</span>
          </div>
        )}

        {issued && (
          <section className="panel" aria-labelledby="issued-title">
            <h2 id="issued-title">{issued.title}</h2>
            <p>次のURLを、安全な方法（直接渡す・本人だけが見られる連絡など）で本人に渡してください。<strong>このURLはこの画面でしか表示されません。</strong>閉じると、もう一度発行するまで確認できません。</p>
            <div className="field">
              <label htmlFor="issued-url">URL（有効期限: {new Date(issued.expiresAt).toLocaleString("ja-JP")}）</label>
              <div className="input-row">
                <input id="issued-url" className="input" readOnly value={issued.url} onFocus={(e) => e.currentTarget.select()} />
                <button
                  type="button"
                  className="toggle"
                  onClick={() => navigator.clipboard?.writeText(issued.url).then(() => setNotice("URLをコピーしました。"), () => setError("コピーできませんでした。URLを選択してコピーしてください。"))}
                >
                  コピー
                </button>
              </div>
            </div>
            <button className="btn secondary inline" onClick={() => setIssued(null)}>
              閉じる
            </button>
          </section>
        )}

        <details className="panel add-panel" open={addOpen} onToggle={(e) => setAddOpen(e.currentTarget.open)}>
          <summary>ユーザーを追加する</summary>
          <AddUser
            onCreated={(r) => {
              showLink(`${r.user?.displayName ?? ""}さんの招待URL`, r);
              setNotice(`${r.user?.loginId ?? ""} を追加しました。`);
              void load(query);
            }}
            onError={(m) => setError(m)}
          />
        </details>

        <section aria-labelledby="list-title">
          <h2 id="list-title">ユーザー一覧</h2>
          <form
            className="search"
            role="search"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              setQuery(q.trim());
              void load(q.trim());
            }}
          >
            <label className="visually-hidden" htmlFor="q">ログインIDまたは表示名で検索</label>
            <input id="q" className="input" type="search" placeholder="ログインIDまたは表示名" value={q} onChange={(e) => setQ(e.target.value)} />
            <button className="btn inline" type="submit">検索</button>
          </form>
          {loading ? (
            <p aria-busy="true">読み込んでいます…</p>
          ) : users.length === 0 ? (
            <p>該当するユーザーはいません。</p>
          ) : (
            <ul className="users">
              {users.map((u) => {
                const self = u.id === me?.user.id;
                const privileged = u.roles.includes("admin") || u.roles.includes("owner");
                const locked = privileged && !isOwner; // 管理者権限の対象はOwnerだけが操作できる（サーバーが強制）
                const busy = busyId === u.id;
                return (
                  <li key={u.id} className="user" data-login={u.loginId}>
                    <div className="user-head">
                      <div>
                        <strong className="user-name">{u.displayName || u.loginId}</strong>
                        <div className="user-id">{u.loginId}{self ? "（あなた）" : ""}</div>
                      </div>
                      <div className="badges">
                        <span className={`badge status-${u.status}`}>{STATUS_LABEL[u.status]}</span>
                        {u.roles.map((r) => (
                          <span key={r} className="badge role">{ROLE_LABEL[r] ?? r}</span>
                        ))}
                      </div>
                    </div>
                    <div className="actions">
                      {u.status === "invited" && (
                        <button className="btn secondary inline" disabled={busy || locked} onClick={() => run(u.id, async () => showLink(`${u.displayName || u.loginId}さんの招待URL`, await issueLink(u.id, "invite-link")), "招待URLを再発行しました（以前のURLは使えません）。")}>
                          招待URLを再発行
                        </button>
                      )}
                      {u.status === "active" && (
                        <button className="btn secondary inline" disabled={busy || locked} onClick={() => run(u.id, async () => showLink(`${u.displayName || u.loginId}さんのパスワード再設定URL`, await issueLink(u.id, "reset-link")), "再設定URLを発行しました。")}>
                          再設定URLを発行
                        </button>
                      )}
                      {u.status !== "disabled" && !self && (
                        confirming === `stop-${u.id}` ? (
                          <span className="confirm" role="group" aria-label={`${u.loginId}を停止する確認`}>
                            <span>停止すると、ログイン中の端末もすべて解除されます。</span>
                            <button className="btn danger inline" disabled={busy} onClick={() => run(u.id, () => setUserStatus(u.id, "disabled"), `${u.loginId} を停止しました。`)}>停止する</button>
                            <button className="btn secondary inline" onClick={() => setConfirming(null)}>やめる</button>
                          </span>
                        ) : (
                          <button className="btn secondary inline" disabled={busy || locked} onClick={() => setConfirming(`stop-${u.id}`)}>停止する</button>
                        )
                      )}
                      {u.status === "disabled" && (
                        <button className="btn secondary inline" disabled={busy || locked} onClick={() => run(u.id, () => setUserStatus(u.id, "active"), `${u.loginId} を再開しました。` )}>再開する</button>
                      )}
                      {isAdmin && !u.roles.includes("owner") && (
                        <button className="btn secondary inline" disabled={busy} onClick={() => run(u.id, () => setRole(u.id, "editor", !u.roles.includes("editor")), u.roles.includes("editor") ? "Editorを外しました。" : "Editorにしました。")}>
                          {u.roles.includes("editor") ? "Editorを外す" : "Editorにする"}
                        </button>
                      )}
                      {isOwner && !u.roles.includes("owner") && (
                        <button className="btn secondary inline" disabled={busy} onClick={() => run(u.id, () => setRole(u.id, "admin", !u.roles.includes("admin")), u.roles.includes("admin") ? "Adminを外しました。" : "Adminにしました。")}>
                          {u.roles.includes("admin") ? "Adminを外す" : "Adminにする"}
                        </button>
                      )}
                    </div>
                    <div className="actions">
                      <button className="btn secondary inline" disabled={busy} onClick={() => run(u.id, async () => setPriv(await getPrivateProfile(u.id)), "個人情報を表示しました（この閲覧は記録されます）。")}>
                        個人情報を見る
                      </button>
                      {u.status === "disabled" && deleting !== u.id && (
                        <button className="btn secondary inline" disabled={busy} onClick={() => { setDeleting(u.id); setReason(""); setAnonymize(true); }}>個人情報を削除する</button>
                      )}
                    </div>
                    {priv && priv.id === u.id && (
                      <div className="panel" role="region" aria-label={`${u.loginId}の個人情報`}>
                        <p className="hint"><strong>この閲覧は監査ログに記録されました。</strong>見終わったら閉じてください。</p>
                        <dl className="facts">
                          <dt>氏名</dt><dd>{priv.private.legalName ?? "（未入力）"}</dd>
                          <dt>大学メール</dt><dd>{priv.private.universityEmail ?? "（未入力）"}</dd>
                          <dt>学生証番号</dt><dd>{priv.private.studentNumber ?? "（未入力）"}</dd>
                          <dt>最寄り駅</dt><dd>{priv.private.nearestStation ?? "（未入力）"}</dd>
                        </dl>
                        <button className="btn secondary inline" onClick={() => setPriv(null)}>閉じる</button>
                      </div>
                    )}
                    {deleting === u.id && (
                      <form
                        className="panel"
                        aria-label={`${u.loginId}の個人情報を削除`}
                        onSubmit={(e) => {
                          e.preventDefault();
                          void run(u.id, async () => { await deletePrivateProfile(u.id, reason.trim(), anonymize); setPriv(null); setDeleting(null); }, `${u.loginId} の個人情報を削除しました。`);
                        }}
                      >
                        <p><strong>この操作は元に戻せません。</strong>氏名・大学メール・学生証番号・最寄り駅・自己紹介・学部学科学年・外部リンク・個人機材・公開設定を削除し、インターネット公開の同意を取り消します。ユーザー本人のアカウントと活動の履歴は残ります。</p>
                        <p className="hint">記事の本文や画像の中に書かれた本人の情報は、自動では消えません。必要に応じて管理者が確認して直してください。</p>
                        <div className="field">
                          <label htmlFor={`reason-${u.id}`}>削除の理由（必須・200文字まで。個人情報は書かないでください）</label>
                          <textarea id={`reason-${u.id}`} className="input" rows={2} value={reason} onChange={(e) => setReason(e.target.value)} required maxLength={200} />
                        </div>
                        <div className="field">
                          <label><input type="checkbox" checked={anonymize} onChange={(e) => setAnonymize(e.target.checked)} /> 表示名も「退会メンバー」に置き換える</label>
                        </div>
                        <div className="actions">
                          <button className="btn danger inline" type="submit" disabled={busy || reason.trim() === ""}>完全に削除する</button>
                          <button className="btn secondary inline" type="button" onClick={() => setDeleting(null)}>やめる</button>
                        </div>
                      </form>
                    )}
                    {locked && <p className="hint">管理者（Admin・Owner）の停止・URL発行は、Ownerだけができます。</p>}
                  </li>
                );
              })}
            </ul>
          )}
          {next && (
            <button
              className="btn secondary inline"
              onClick={async () => {
                try {
                  const r = await listUsers(query, next);
                  setUsers((cur) => [...cur, ...r.items]);
                  setNext(r.nextCursor);
                } catch (e) {
                  setError(describe(e));
                }
              }}
            >
              さらに表示
            </button>
          )}
        </section>
      </main>
    </>
  );
}

function AddUser({ onCreated, onError }: { onCreated: (r: LinkResult) => void; onError: (m: string) => void }) {
  const [loginId, setLoginId] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault();
        if (busy) return;
        setBusy(true);
        try {
          const r = await createUser(loginId.trim(), displayName.trim());
          onCreated(r);
          setLoginId("");
          setDisplayName("");
        } catch (err) {
          onError(describe(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <div className="field">
        <label htmlFor="new-login">ログインID</label>
        <input id="new-login" className="input" value={loginId} onChange={(e) => setLoginId(e.target.value)} required autoCapitalize="none" spellCheck={false} />
        <p className="hint">小文字の英数字と . _ - が使えます（2〜63文字、最初は英数字）。本人は変更できません。</p>
      </div>
      <div className="field">
        <label htmlFor="new-name">表示名</label>
        <input id="new-name" className="input" value={displayName} onChange={(e) => setDisplayName(e.target.value)} required maxLength={64} />
        <p className="hint">メンバー一覧などに表示される名前です（64文字まで）。本人があとで変更できます。</p>
      </div>
      <button className="btn inline" type="submit" disabled={busy}>
        {busy ? "追加しています…" : "追加して招待URLを発行する"}
      </button>
    </form>
  );
}
