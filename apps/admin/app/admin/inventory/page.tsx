"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import ItemImagesEditor from "@/components/ItemImagesEditor";
import MediaImg from "@/components/MediaImg";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import Select from "@/components/ui/Select";
import { Loading, EmptyState } from "@/components/ui/States";
import TextField from "@/components/ui/TextField";
import Textarea from "@/components/ui/Textarea";
import { useToast } from "@/components/ui/Toast";
import { createItem, describe, listCategories, listItems, type Category, type Item } from "@/lib/api";
import { commitImages, type ImageEntry } from "@/lib/item-images";

export default function AdminInventoryPage() {
  return <AppShell>{() => <AdminInventory />}</AppShell>;
}

function AdminInventory() {
  const toast = useToast();
  const [items, setItems] = useState<Item[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [f, setF] = useState({ name: "", categoryId: "", location: "", description: "", mode: "quantity" as "quantity" | "individual", total: 1 });
  const [images, setImages] = useState<ImageEntry[]>([]);
  const [version, setVersion] = useState(0);

  useEffect(() => {
    let alive = true;
    Promise.all([listItems({}), listCategories()])
      .then(([r, c]) => {
        if (!alive) return;
        setItems(r.items);
        setCategories(c.items);
        setF((cur) => (cur.categoryId ? cur : { ...cur, categoryId: c.items.find((x) => x.name === "その他")?.id ?? c.items[0]?.id ?? "" }));
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
  }, [version]);

  async function onCreate(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    let created: Item | null = null;
    try {
      created = await createItem({ name: f.name.trim(), categoryId: f.categoryId, location: f.location.trim(), description: f.description.trim(), mode: f.mode, ...(f.mode === "quantity" ? { totalQuantity: f.total } : {}) });
      if (images.length > 0) await commitImages(created.id, images);
      toast("success", `「${created.name}」を登録しました。${f.mode === "individual" ? "個体（管理番号）は編集画面から登録してください。" : ""}`);
      setF({ ...f, name: "", description: "", location: "" });
      setImages([]);
      setVersion((v) => v + 1);
    } catch (e2) {
      // 物品は登録できたが写真の保存に失敗した場合は、編集画面で続きができることを伝える
      setError(created ? `物品は登録しましたが、写真の保存に失敗しました。編集画面から写真をもう一度追加してください。（${describe(e2)}）` : describe(e2));
      if (created) setVersion((v) => v + 1);
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="page wide">
      <p><Link href="/inventory">← 物品一覧へ</Link></p>
      <div className="page-head">
        <h1>物品の管理</h1>
        <nav className="subnav" aria-label="管理のメニュー">
          <Button variant="default" fullWidth={false} href="/admin/inventory/categories">カテゴリの管理</Button>
          <Button variant="default" fullWidth={false} href="/admin/notifications">通知の設定</Button>
        </nav>
      </div>
      <p className="lead">物品の登録・編集は管理者（Admin・Owner）だけができます。</p>
      {error && <Notice kind="error">{error}</Notice>}
      <section className="panel" aria-labelledby="new-title">
        <h2 id="new-title">物品を登録する</h2>
        <form onSubmit={onCreate}>
          <div className="field"><TextField id="n-name" label="名前（必須）" required maxLength={100} value={f.name} onChange={(value: string) => setF({ ...f, name: value })} /></div>
          <Select id="n-cat" label="カテゴリ" value={f.categoryId} onChange={(v) => setF({ ...f, categoryId: v })}>
            {categories.map((c) => (<option key={c.id} value={c.id}>{c.name}</option>))}
          </Select>
          <div className="field"><TextField id="n-loc" label="保管場所" maxLength={50} value={f.location} onChange={(value: string) => setF({ ...f, location: value })} /></div>
          <Textarea id="n-desc" label="説明" rows={3} maxLength={1000} value={f.description} onChange={(v) => setF({ ...f, description: v })} />
          <Select id="n-mode" label="管理方式" value={f.mode} onChange={(v) => setF({ ...f, mode: v as "quantity" | "individual" })}>
            <option value="quantity">数量で管理（ケーブルなど）</option>
            <option value="individual">個体ごとに管理（管理番号をつける機材）</option>
          </Select>
          {f.mode === "quantity" && (
            <div className="field"><label htmlFor="n-total">総数</label><input id="n-total" className="input" type="number" min={1} inputMode="numeric" value={f.total} onChange={(e) => setF({ ...f, total: Math.max(1, Number(e.target.value) || 1) })} /></div>
          )}
          <div className="field">
            <span className="label">写真</span>
            <ItemImagesEditor entries={images} onChange={setImages} onError={setError} disabled={busy} />
          </div>
          <Button fullWidth={false} type="submit" disabled={busy || !f.name.trim() || !f.categoryId}>{busy ? "登録しています…" : "登録する"}</Button>
        </form>
      </section>
      <section aria-labelledby="list-title">
        <h2 id="list-title">登録済みの物品</h2>
        {loading ? <Loading /> : items.length === 0 ? <EmptyState title="まだ物品がありません">上のフォームから登録できます。</EmptyState> : (
          <ul className="items">
            {items.map((it) => (
              <li key={it.id} className="item admin-item" data-name={it.name}>
                <div className="admin-item-thumb">{it.photoMediaId ? <MediaImg id={it.photoMediaId} lazy /> : <span aria-hidden="true">{Array.from(it.name)[0]}</span>}</div>
                <div className="grow"><strong>{it.name}</strong><div className="user-id">{it.category} ・ {it.mode === "quantity" ? `数量管理 ・ 総数${it.totalQuantity}` : `個体管理 ・ ${it.unitCount}個体`}</div></div>
                <Button variant="default" fullWidth={false} href={`/admin/inventory/${it.id}`}>編集・個体</Button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
