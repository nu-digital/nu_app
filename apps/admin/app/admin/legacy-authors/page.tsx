"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import MemberPicker from "@/components/MemberPicker";
import Notice from "@/components/Notice";
import {
  describe, linkLegacyAuthor, listLegacyAuthors, permitLegacyAuthor, unlinkLegacyAuthor, withdrawLegacyAuthor,
  type LegacyAuthor,
} from "@/lib/api";

export default function LegacyAuthorsPage() {
  return <AppShell>{() => <LegacyAuthors />}</AppShell>;
}

const STATE: Record<LegacyAuthor["publicationState"], { label: string; badge: string }> = {
  unconfirmed: { label: "未確認（公開サイトでは「nu_digital」）", badge: "status-invited" },
  permitted: { label: "公開を許可（名前を表示）", badge: "status-active" },
  withdrawn: { label: "公開しない（「nu_digital」）", badge: "status-disabled" },
};

function LegacyAuthors() {
  const [items, setItems] = useState<LegacyAuthor[]>([]);
  const [loading, setLoading] = useState(true);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const reload = useCallback(async () => {
    try {
      setItems((await listLegacyAuthors()).items);
    } catch (e) {
      setMsg({ kind: "error", text: describe(e) });
    }
  }, []);
  useEffect(() => {
    let alive = true;
    listLegacyAuthors()
      .then((r) => { if (!alive) return; setItems(r.items); setLoading(false); })
      .catch((e) => { if (!alive) return; setMsg({ kind: "error", text: describe(e) }); setLoading(false); });
    return () => { alive = false; };
  }, []);
  return (
    <main className="page wide">
      <p><Link href="/admin/users">← ユーザー管理へ</Link></p>
      <h1>既存記事の投稿者</h1>
      <p className="lead">移行した記事の投稿者名です。<strong>本人が公開を許可したことを確認して記録するまで、公開サイトには出ません</strong>（「nu_digital」と表示されます）。アカウントとの紐付けは、公開の許可とは別の操作で、どちらも相手を自動では変えません。表示名が同じでも自動では紐付きません。</p>
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}
      {loading ? <p aria-busy="true">読み込んでいます…</p> : (
        <ul className="items">
          {items.map((a) => <AuthorRow key={a.id} a={a} onMsg={setMsg} onChanged={reload} />)}
        </ul>
      )}
    </main>
  );
}

function AuthorRow({ a, onMsg, onChanged }: { a: LegacyAuthor; onMsg: (m: { kind: "error" | "success"; text: string } | null) => void; onChanged: () => Promise<void> }) {
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [picking, setPicking] = useState(false);
  const run = async (f: () => Promise<void>, done: string) => {
    if (busy) return;
    setBusy(true);
    onMsg(null);
    try {
      await f();
      onMsg({ kind: "success", text: done });
      setNote("");
      setPicking(false);
      await onChanged();
    } catch (e) {
      onMsg({ kind: "error", text: describe(e) });
    } finally {
      setBusy(false);
    }
  };
  const st = STATE[a.publicationState];
  const nid = `note-${a.id}`;
  return (
    <li className="item" data-author={a.erased ? "erased" : a.name}>
      <section aria-labelledby={`h-${a.id}`}>
        <h2 id={`h-${a.id}`}>{a.erased ? "（個人情報を削除済み）" : a.name} <span className={`badge ${st.badge}`}>{st.label}</span></h2>
        <p className="hint">記事 {a.contentCount} 件{a.confirmedAt && a.publicationState === "permitted" ? ` ／ 許可の確認: ${new Date(a.confirmedAt).toLocaleDateString("ja-JP")}` : ""}</p>
        <p className="hint">紐付け: {a.linkedUserId ? `${a.linkedDisplayName ?? "ユーザー"}さんのアカウント` : "なし"}</p>
        {!a.erased && (
          <>
            {a.publicationState !== "permitted" && (
              <div className="field">
                <label htmlFor={nid}>本人の許可をどう確認したか（200文字以内。個人情報は書かない）</label>
                <input id={nid} className="input" value={note} maxLength={200} onChange={(e) => setNote(e.target.value)} />
                <button type="button" className="btn inline" disabled={busy || note.trim() === ""} onClick={() => run(() => permitLegacyAuthor(a.id, note.trim()), "公開の許可を記録しました。")}>本人の許可を確認した（公開する）</button>
              </div>
            )}
            {a.publicationState === "permitted" && (
              <button type="button" className="btn secondary inline" disabled={busy} onClick={() => run(() => withdrawLegacyAuthor(a.id), "公開を止めました。")}>公開を止める</button>
            )}
            <div className="actions">
              {a.linkedUserId
                ? <button type="button" className="btn secondary inline" disabled={busy} onClick={() => run(() => unlinkLegacyAuthor(a.id), "紐付けを外しました（公開の可否は変わりません）。")}>紐付けを外す</button>
                : <button type="button" className="btn secondary inline" disabled={busy} onClick={() => setPicking((v) => !v)}>アカウントに紐付ける</button>}
            </div>
            {picking && !a.linkedUserId && (
              <MemberPicker label="紐付けるメンバー（本人であることを確認してから選ぶ）" exclude={[]} disabled={busy} onPick={(m) => run(() => linkLegacyAuthor(a.id, m.id), `${m.displayName}さんに紐付けました（公開の可否は変わりません）。`)} />
            )}
          </>
        )}
      </section>
    </li>
  );
}
