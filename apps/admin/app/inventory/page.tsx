"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import MediaImg from "@/components/MediaImg";
import { describe, listItems, type Item } from "@/lib/api";

export default function InventoryPage() {
  return <AppShell>{(me) => <Inventory canManage={me.user.roles.includes("admin") || me.user.roles.includes("owner")} />}</AppShell>;
}

function availability(it: Item) {
  if (it.available > 0) {
    const total = it.mode === "quantity" ? it.totalQuantity : it.unitCount;
    return { text: `残り ${it.available}${total ? ` / ${total}` : ""}`, ok: true };
  }
  return { text: it.mode === "individual" && it.unitCount === 0 ? "個体が未登録" : "貸出中", ok: false };
}

function Inventory({ canManage }: { canManage: boolean }) {
  const [items, setItems] = useState<Item[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [category, setCategory] = useState("");
  const [onlyAvailable, setOnlyAvailable] = useState(false);
  const [applied, setApplied] = useState({ q: "", category: "", onlyAvailable: false });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    listItems({ q: applied.q, category: applied.category, available: applied.onlyAvailable })
      .then((r) => {
        if (!alive) return;
        setItems(r.items);
        setNext(r.nextCursor);
        setCategories(r.categories);
        setError("");
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
  }, [applied]);

  return (
    <main className="page wide">
      <h1>物品</h1>
      <nav className="subnav" aria-label="物品のメニュー">
        <Link className="btn secondary inline" href="/inventory/loans">貸出中の一覧</Link>
        <Link className="btn secondary inline" href="/inventory/history">履歴</Link>
        {canManage && <Link className="btn secondary inline" href="/admin/inventory">物品の管理</Link>}
      </nav>
      {error && <Notice kind="error">{error}</Notice>}
      <form className="filters" role="search" onSubmit={(e) => { e.preventDefault(); setLoading(true); setApplied({ q: q.trim(), category, onlyAvailable }); }}>
        <div className="field">
          <label htmlFor="iq">物品を探す</label>
          <input id="iq" className="input" type="search" placeholder="名前・場所・カテゴリ" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="icat">カテゴリ</label>
          <select id="icat" className="input" value={category} onChange={(e) => setCategory(e.target.value)}>
            <option value="">すべて</option>
            {categories.map((c) => (<option key={c} value={c}>{c}</option>))}
          </select>
        </div>
        <div className="field">
          <label><input type="checkbox" checked={onlyAvailable} onChange={(e) => setOnlyAvailable(e.target.checked)} /> 借りられるものだけ</label>
        </div>
        <button className="btn inline" type="submit">絞り込む</button>
      </form>
      {loading ? <p aria-busy="true">読み込んでいます…</p> : items.length === 0 ? <p>該当する物品はありません。</p> : (
        <ul className="grid">
          {items.map((it) => {
            const a = availability(it);
            return (
              <li key={it.id}>
                <Link className="item-card" href={`/inventory/${it.id}`} data-name={it.name}>
                  <div className="thumb" aria-hidden="true">{it.photoMediaId ? <MediaImg id={it.photoMediaId} lazy /> : it.name.slice(0, 1)}</div>
                  <div className="item-body">
                    <h2>{it.name}</h2>
                    <p className="meta">{[it.category, it.location].filter(Boolean).join(" ・ ") || "　"}</p>
                    <span className={`badge ${a.ok ? "status-active" : "status-disabled"}`}>{a.text}</span>
                  </div>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
      {next && <button className="btn secondary inline" onClick={async () => { try { const r = await listItems({ ...applied, available: applied.onlyAvailable, cursor: next }); setItems((c) => [...c, ...r.items]); setNext(r.nextCursor); } catch (e) { setError(describe(e)); } }}>さらに表示</button>}
    </main>
  );
}
