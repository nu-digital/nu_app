"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import MediaImg from "@/components/MediaImg";
import { describe, listMembers, type Member } from "@/lib/api";

export default function MembersPage() {
  return <AppShell>{() => <Members />}</AppShell>;
}

function Members() {
  const [items, setItems] = useState<Member[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async (search: string) => {
    setLoading(true);
    try {
      const r = await listMembers(search);
      setItems(r.items);
      setNext(r.nextCursor);
      setError("");
    } catch (e) {
      setError(describe(e));
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    let alive = true;
    listMembers("")
      .then((r) => {
        if (!alive) return;
        setItems(r.items);
        setNext(r.nextCursor);
        setLoading(false);
      })
      .catch((e) => {
        if (!alive) return;
        setError(describe(e));
        setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, []);

  return (
    <main className="page">
      <h1>メンバー</h1>
      <p className="lead">いま活動しているメンバーです。各自が「メンバーに公開」にした情報だけが表示されます。</p>
      {error && <Notice kind="error">{error}</Notice>}
      <form className="search" role="search" onSubmit={(e) => { e.preventDefault(); setQuery(q.trim()); void load(q.trim()); }}>
        <label className="visually-hidden" htmlFor="mq">表示名で検索</label>
        <input id="mq" className="input" type="search" placeholder="表示名で検索" value={q} onChange={(e) => setQ(e.target.value)} />
        <button className="btn inline" type="submit">検索</button>
      </form>
      {loading ? <p aria-busy="true">読み込んでいます…</p> : items.length === 0 ? <p>該当するメンバーはいません。</p> : (
        <ul className="users">
          {items.map((m) => (
            <li key={m.id} className="user" data-name={m.displayName}>
              <div className="member-row">
                {m.avatarMediaId && <MediaImg className="avatar" id={m.avatarMediaId} lazy />}
                <Link href={`/members/${m.id}`} className="member-link"><strong className="user-name">{m.displayName}</strong></Link>
              </div>
              <div className="user-id">{[m.faculty, m.department, m.schoolYear ? `${m.schoolYear}年` : null].filter(Boolean).join(" ")}</div>
              {m.bio && <p className="bio">{m.bio}</p>}
            </li>
          ))}
        </ul>
      )}
      {next && <button className="btn secondary inline" onClick={async () => { try { const r = await listMembers(query, next); setItems((c) => [...c, ...r.items]); setNext(r.nextCursor); } catch (e) { setError(describe(e)); } }}>さらに表示</button>}
    </main>
  );
}
