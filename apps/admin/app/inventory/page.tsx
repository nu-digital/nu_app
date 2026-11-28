"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";

import AppShell from "@/components/AppShell";
import ItemStatusBadges from "@/components/ItemStatus";
import MediaImg from "@/components/MediaImg";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import Select from "@/components/ui/Select";
import { CardSkeleton, EmptyState } from "@/components/ui/States";
import { describe, listItems, type Category, type Item, type ItemSort } from "@/lib/api";
import { itemStatus } from "@/lib/item-status";

export default function InventoryPage() {
  return <AppShell>{(me) => <Inventory canManage={me.user.roles.includes("admin") || me.user.roles.includes("owner")} />}</AppShell>;
}

type Filters = { q: string; categoryId: string; onlyAvailable: boolean; mine: boolean; sort: ItemSort };
const INITIAL: Filters = { q: "", categoryId: "", onlyAvailable: false, mine: false, sort: "name" };

/** カードの写真に重ねる状態の札（メルカリのSOLDのように、借りられない物がひと目で分かる） */
function Ribbon({ it }: { it: Item }) {
  const s = itemStatus(it);
  if (s.key === "none") return <span className="ribbon ribbon-none"><span aria-hidden="true">✕ </span>貸出中</span>;
  if (s.key === "empty") return <span className="ribbon ribbon-none">準備中</span>;
  return null;
}

function Inventory({ canManage }: { canManage: boolean }) {
  const [items, setItems] = useState<Item[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [text, setText] = useState("");
  const [f, setF] = useState<Filters>(INITIAL);
  const [loadedKey, setLoadedKey] = useState("");
  const [more, setMore] = useState(false);
  const [error, setError] = useState("");
  const firstLoad = useRef(true);

  // 検索語は入力が止まってから反映する（打つたびに通信しない）
  useEffect(() => {
    const t = setTimeout(() => setF((cur) => (cur.q === text.trim() ? cur : { ...cur, q: text.trim() })), firstLoad.current ? 0 : 350);
    return () => clearTimeout(t);
  }, [text]);

  const key = JSON.stringify(f);
  const loading = loadedKey !== key;
  useEffect(() => {
    let alive = true;
    listItems({ q: f.q, categoryId: f.categoryId, available: f.onlyAvailable, mine: f.mine, sort: f.sort })
      .then((r) => {
        if (!alive) return;
        firstLoad.current = false;
        setItems(r.items);
        setNext(r.nextCursor);
        setCategories(r.categoryList);
        setError("");
        setLoadedKey(key);
      })
      .catch((e) => {
        if (!alive) return;
        setError(describe(e));
        setLoadedKey(key);
      });
    return () => {
      alive = false;
    };
  }, [f, key]);

  const set = useCallback((patch: Partial<Filters>) => setF((cur) => ({ ...cur, ...patch })), []);
  const filtered = f.q !== "" || f.categoryId !== "" || f.onlyAvailable || f.mine;

  async function loadMore() {
    if (!next || more) return;
    setMore(true);
    try {
      const r = await listItems({ q: f.q, categoryId: f.categoryId, available: f.onlyAvailable, mine: f.mine, sort: f.sort, cursor: next });
      setItems((c) => [...c, ...r.items]);
      setNext(r.nextCursor);
    } catch (e) {
      setError(describe(e));
    } finally {
      setMore(false);
    }
  }

  return (
    <main className="page wide">
      <div className="page-head">
        <h1>物品</h1>
        <nav className="subnav" aria-label="物品のメニュー">
          <Button variant="default" fullWidth={false} href="/inventory/loans">貸出中の一覧</Button>
          <Button variant="default" fullWidth={false} href="/inventory/history">履歴</Button>
          {canManage && <Button variant="default" fullWidth={false} href="/admin/inventory">物品の管理</Button>}
        </nav>
      </div>
      {error && <Notice kind="error">{error}</Notice>}

      <div className="toolbar" role="search">
        <div className="field search-field">
          <label htmlFor="iq" className="visually-hidden">物品を探す</label>
          <input id="iq" className="input" type="search" placeholder="物品を探す（名前・場所・説明）" value={text} onChange={(e) => setText(e.target.value)} />
        </div>
        <Select id="isort" label="並び替え" hideLabel value={f.sort} onChange={(v) => set({ sort: v as ItemSort })}>
          <option value="name">名前順</option>
          <option value="available">借りられる数が多い順</option>
          <option value="newest">新しい順</option>
        </Select>
      </div>
      <div className="chips" role="group" aria-label="カテゴリ">
        <button type="button" className="chip" aria-pressed={f.categoryId === ""} onClick={() => set({ categoryId: "" })}>すべて</button>
        {categories.map((c) => (
          <button key={c.id} type="button" className="chip" aria-pressed={f.categoryId === c.id} onClick={() => set({ categoryId: f.categoryId === c.id ? "" : c.id })}>{c.name}</button>
        ))}
        <span className="chip-sep" aria-hidden="true" />
        <button type="button" className="chip" aria-pressed={f.onlyAvailable} onClick={() => set({ onlyAvailable: !f.onlyAvailable })}>借りられる物だけ</button>
        <button type="button" className="chip" aria-pressed={f.mine} onClick={() => set({ mine: !f.mine })}>自分が借りている物</button>
      </div>

      <p className="result-count" aria-live="polite">{loading ? "" : `${items.length}${next ? "件以上" : "件"}の物品`}</p>
      {loading ? <CardSkeleton /> : items.length === 0 ? (
        <EmptyState
          title={filtered ? "条件に合う物品はありません" : "まだ物品が登録されていません"}
          action={filtered ? <Button variant="default" fullWidth={false} onClick={() => { setText(""); setF(INITIAL); }}>条件をリセット</Button> : canManage ? <Button fullWidth={false} href="/admin/inventory">物品を登録する</Button> : undefined}
        >
          {filtered ? "検索語やカテゴリを変えてみてください。" : canManage ? "管理画面から物品と写真を登録できます。" : "管理者が登録すると、ここに表示されます。"}
        </EmptyState>
      ) : (
        <ul className="grid">
          {items.map((it) => {
            const s = itemStatus(it);
            return (
              <li key={it.id}>
                <Link className={`item-card status-${s.key}`} href={`/inventory/${it.id}`} data-name={it.name}>
                  <div className="thumb">
                    {it.photoMediaId ? <MediaImg id={it.photoMediaId} lazy /> : <span className="thumb-empty" aria-hidden="true">{Array.from(it.name)[0]}</span>}
                    <Ribbon it={it} />
                  </div>
                  <div className="item-body">
                    <h2>{it.name}</h2>
                    <p className="meta">{[it.category, it.location].filter(Boolean).join(" ・ ") || "　"}</p>
                    <ItemStatusBadges item={it} />
                  </div>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
      {next && <div className="center"><Button variant="default" fullWidth={false} disabled={more} onClick={loadMore}>{more ? "読み込んでいます…" : "さらに表示"}</Button></div>}
    </main>
  );
}
