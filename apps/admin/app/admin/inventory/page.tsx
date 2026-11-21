"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import { createItem, describe, listItems, type Item } from "@/lib/api";

export default function AdminInventoryPage() {
  return <AppShell>{() => <AdminInventory />}</AppShell>;
}

function AdminInventory() {
  const [items, setItems] = useState<Item[]>([]);
  const [loading, setLoading] = useState(true);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [f, setF] = useState({ name: "", category: "", location: "", description: "", mode: "quantity" as "quantity" | "individual", total: 1 });

  const [version, setVersion] = useState(0);
  useEffect(() => {
    let alive = true;
    listItems({})
      .then((r) => {
        if (!alive) return;
        setItems(r.items);
        setLoading(false);
      })
      .catch((e) => {
        if (!alive) return;
        setMsg({ kind: "error", text: describe(e) });
        setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [version]);

  async function onCreate(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setMsg(null);
    try {
      const it = await createItem({ name: f.name.trim(), category: f.category.trim(), location: f.location.trim(), description: f.description.trim(), mode: f.mode, ...(f.mode === "quantity" ? { totalQuantity: f.total } : {}) });
      setMsg({ kind: "success", text: `「${it.name}」を登録しました。${f.mode === "individual" ? "個体（管理番号）は物品の管理画面から登録してください。" : ""}` });
      setF({ ...f, name: "", description: "" });
      setVersion((v) => v + 1);
    } catch (e2) {
      setMsg({ kind: "error", text: describe(e2) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="page wide">
      <p><Link href="/inventory">← 物品一覧へ</Link></p>
      <h1>物品の管理</h1>
      <nav className="subnav" aria-label="管理のメニュー"><Link className="btn secondary inline" href="/admin/notifications">通知の設定</Link></nav>
      <p className="lead">物品の登録・編集は管理者（Admin・Owner）だけができます。権限が無い場合は登録時に案内が出ます。</p>
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}
      <section className="panel" aria-labelledby="new-title">
        <h2 id="new-title">物品を登録する</h2>
        <form onSubmit={onCreate}>
          <div className="field"><label htmlFor="n-name">名前（必須）</label><input id="n-name" className="input" required maxLength={100} value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></div>
          <div className="field"><label htmlFor="n-cat">カテゴリ</label><input id="n-cat" className="input" maxLength={50} value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })} /></div>
          <div className="field"><label htmlFor="n-loc">保管場所</label><input id="n-loc" className="input" maxLength={100} value={f.location} onChange={(e) => setF({ ...f, location: e.target.value })} /></div>
          <div className="field"><label htmlFor="n-desc">説明</label><textarea id="n-desc" className="input" rows={3} maxLength={1000} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></div>
          <div className="field">
            <label htmlFor="n-mode">管理方式</label>
            <select id="n-mode" className="input" value={f.mode} onChange={(e) => setF({ ...f, mode: e.target.value as "quantity" | "individual" })}>
              <option value="quantity">数量で管理（ケーブルなど）</option>
              <option value="individual">個体ごとに管理（管理番号をつける機材）</option>
            </select>
          </div>
          {f.mode === "quantity" && (
            <div className="field"><label htmlFor="n-total">総数</label><input id="n-total" className="input" type="number" min={1} inputMode="numeric" value={f.total} onChange={(e) => setF({ ...f, total: Math.max(1, Number(e.target.value) || 1) })} /></div>
          )}
          <button className="btn inline" type="submit" disabled={busy || !f.name.trim()}>登録する</button>
        </form>
      </section>
      <section aria-labelledby="list-title">
        <h2 id="list-title">登録済みの物品</h2>
        {loading ? <p aria-busy="true">読み込んでいます…</p> : items.length === 0 ? <p>まだ物品がありません。</p> : (
          <ul className="items">
            {items.map((it) => (
              <li key={it.id} className="item" data-name={it.name}>
                <div><strong>{it.name}</strong><div className="user-id">{it.mode === "quantity" ? `数量管理 ・ 総数${it.totalQuantity}` : `個体管理 ・ ${it.unitCount}個体`}</div></div>
                <Link className="btn secondary inline" href={`/admin/inventory/${it.id}`}>編集・個体</Link>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
