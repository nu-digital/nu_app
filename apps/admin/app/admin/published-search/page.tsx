"use client";

import Link from "next/link";
import { useState, type FormEvent } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import { describe, searchPublished, type PublishedHit } from "@/lib/api";

export default function PublishedSearchPage() {
  return <AppShell>{() => <Search />}</AppShell>;
}

const FIELD: Record<PublishedHit["found"][number], string> = { title: "題名", summary: "要約", markdown: "本文", roleLabel: "制作メンバーの役割名" };

function Search() {
  const [q, setQ] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [result, setResult] = useState<{ q: string; items: PublishedHit[]; truncated: boolean } | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setMsg(null);
    try {
      const r = await searchPublished(q.trim());
      setResult({ q: q.trim(), items: r.items, truncated: r.truncated });
    } catch (err) {
      setResult(null);
      setMsg(describe(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="page wide">
      <p><Link href="/admin/users">← ユーザー管理へ</Link></p>
      <h1>公開版に残る本人情報の確認</h1>
      <p className="lead">個人情報の削除や公開同意の撤回の依頼があったとき、<strong>公開中</strong>の記事・作品の題名・要約・本文・制作メンバーの役割名に、その人の名前などが残っていないかを探します。見つかったら、記事を編集して公開し直してください（以前の内容は公開されませんが、書き換えられません）。探した語は記録に残りません（探したことだけが残ります）。</p>
      <form className="input-row" onSubmit={submit} role="search">
        <label className="visually-hidden" htmlFor="pq">探す語（2〜50文字）</label>
        <input id="pq" className="input" type="search" required minLength={2} maxLength={50} placeholder="名前など" value={q} onChange={(e) => setQ(e.target.value)} />
        <Button fullWidth={false} type="submit" disabled={busy}>探す</Button>
      </form>
      {msg && <Notice kind="error">{msg}</Notice>}
      {result && (
        result.items.length === 0 ? <p data-testid="no-hits">公開中の記事・作品には、「{result.q}」は見つかりませんでした。</p> : (
          <>
            <p data-testid="hit-count">{result.items.length}件{result.truncated ? "（先頭の100件のみ）" : ""}</p>
            <ul className="items">
              {result.items.map((h) => (
                <li key={`${h.section}/${h.slug}`} className="item" data-slug={h.slug}>
                  <div className="grow">
                    <strong>{h.title}</strong>
                    <div className="user-id">/{h.section}/{h.slug}/ ・ {h.found.map((f) => FIELD[f]).join("・")}</div>
                    {h.snippet && <p className="hint">…{h.snippet}…</p>}
                  </div>
                </li>
              ))}
            </ul>
          </>
        )
      )}
    </main>
  );
}
