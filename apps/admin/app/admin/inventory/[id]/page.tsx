"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import ItemImagesEditor from "@/components/ItemImagesEditor";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import Select from "@/components/ui/Select";
import { Loading } from "@/components/ui/States";
import TextField from "@/components/ui/TextField";
import Textarea from "@/components/ui/Textarea";
import { useToast } from "@/components/ui/Toast";
import { createUnit, describe, getItem, listCategories, updateItem, type Category, type ItemDetail } from "@/lib/api";
import { commitImages, fromExisting, type ImageEntry } from "@/lib/item-images";

export default function AdminItemPage() {
  return <AppShell>{() => <Edit />}</AppShell>;
}

function Edit() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const toast = useToast();
  const [categories, setCategories] = useState<Category[]>([]);
  const [images, setImages] = useState<ImageEntry[]>([]);
  const [version, setVersion] = useState(0);
  const [it, setIt] = useState<ItemDetail | null>(null);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [f, setF] = useState({ name: "", categoryId: "", location: "", description: "" });
  const [total, setTotal] = useState(1);
  const [reason, setReason] = useState("");
  const [tag, setTag] = useState("");
  const [serial, setSerial] = useState("");

  useEffect(() => {
    let alive = true;
    Promise.all([getItem(id), listCategories()])
      .then(([v, c]) => {
        if (!alive) return;
        setIt(v);
        setCategories(c.items);
        setF({ name: v.name, categoryId: v.categoryId ?? "", location: v.location, description: v.description });
        setImages(fromExisting(v.images));
        setTotal(v.totalQuantity);
      })
      .catch((e) => alive && setMsg({ kind: "error", text: (e as { status?: number }).status === 404 ? "この物品は見つかりません。" : describe(e) }));
    return () => {
      alive = false;
    };
  }, [id, version]);

  const act = async (f2: () => Promise<string>) => {
    if (busy) return;
    setBusy(true);
    setMsg(null);
    try {
      const text = await f2();
      setVersion((v) => v + 1);
      toast("success", text);
    } catch (e) {
      setMsg({ kind: "error", text: describe(e) });
    } finally {
      setBusy(false);
    }
  };

  if (!it) return <main className="page">{msg ? <Notice kind={msg.kind}>{msg.text}</Notice> : <Loading />}<p><Link href="/admin/inventory">← 物品の管理へ</Link></p></main>;
  const imagesChanged = JSON.stringify(images.map((e) => (e.kind === "existing" ? e.mediaId : "new"))) !== JSON.stringify(it.images) || images.some((e) => e.kind === "new");
  const outstanding = it.outstanding;
  return (
    <main className="page">
      <p><Link href="/admin/inventory">← 物品の管理へ</Link></p>
      <h1>{it.name}</h1>
      <p className="meta">{it.mode === "quantity" ? "数量で管理" : "個体ごとに管理"}（管理方式は、貸出や個体ができたあとは変えられません）</p>
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}
      <section className="panel" aria-labelledby="photo-title">
        <h2 id="photo-title">写真</h2>
        <p className="hint">物品の一覧・詳細に、ログイン中のメンバーへ表示されます（インターネットには公開されません）。先頭の写真が一覧に出ます。</p>
        <ItemImagesEditor entries={images} onChange={setImages} onError={(text) => setMsg({ kind: "error", text })} disabled={busy} />
        <div className="actions">
          <Button fullWidth={false} type="button" disabled={busy || !imagesChanged} onClick={() => act(async () => { await commitImages(id, images); return "写真を保存しました。"; })}>写真を保存する</Button>
          {imagesChanged && <span className="hint">保存していない変更があります</span>}
        </div>
      </section>

      <section className="panel" aria-labelledby="basic-title">
        <h2 id="basic-title">基本情報</h2>
        <form onSubmit={(e) => { e.preventDefault(); void act(async () => { await updateItem(id, { name: f.name.trim(), categoryId: f.categoryId, location: f.location.trim(), description: f.description.trim() }); return "基本情報を保存しました。"; }); }}>
          <div className="field"><TextField id="e-name" label="名前（必須）" required maxLength={100} value={f.name} onChange={(value: string) => setF({ ...f, name: value })} /></div>
          <Select id="e-cat" label="カテゴリ" value={f.categoryId} onChange={(v) => setF({ ...f, categoryId: v })}>
            {categories.map((c) => (<option key={c.id} value={c.id}>{c.name}</option>))}
          </Select>
          <div className="field"><TextField id="e-loc" label="保管場所" maxLength={50} value={f.location} onChange={(value: string) => setF({ ...f, location: value })} /></div>
          <Textarea id="e-desc" label="説明" rows={3} maxLength={1000} value={f.description} onChange={(v) => setF({ ...f, description: v })} />
          <Button fullWidth={false} type="submit" disabled={busy || !f.name.trim()}>保存する</Button>
        </form>
      </section>

      {it.mode === "quantity" ? (
        <section className="panel" aria-labelledby="total-title">
          <h2 id="total-title">総数を変える</h2>
          <p className="hint">現在の総数は{it.totalQuantity}個、貸出中は{outstanding}個です。貸出中の数より少なくはできません。変更は理由つきで履歴に残ります。</p>
          <form onSubmit={(e) => { e.preventDefault(); void act(async () => { await updateItem(id, { totalQuantity: total, reason: reason.trim() }); setReason(""); return `総数を${total}個に変更しました。`; }); }}>
            <div className="field"><label htmlFor="t-total">新しい総数</label><input id="t-total" className="input" type="number" min={1} inputMode="numeric" value={total} onChange={(e) => setTotal(Number(e.target.value) || 1)} /></div>
            <div className="field"><TextField id="t-reason" label="変更の理由（必須）" maxLength={200} value={reason} onChange={(v: string) => setReason(v)} /></div>
            <Button fullWidth={false} type="submit" disabled={busy || !reason.trim() || total === it.totalQuantity}>総数を変更する</Button>
          </form>
        </section>
      ) : (
        <section className="panel" aria-labelledby="unit-title">
          <h2 id="unit-title">個体を登録する</h2>
          <ul className="items">
            {it.units.map((u) => (
              <li key={u.id} className="item" data-tag={u.assetTag}><div><strong>{u.assetTag}</strong><div className="user-id">{u.loaned ? `貸出中: ${u.loaned.borrowerDisplayName}さん` : "在庫あり"}</div></div></li>
            ))}
            {it.units.length === 0 && <li className="hint">個体がまだありません。</li>}
          </ul>
          <form onSubmit={(e) => { e.preventDefault(); void act(async () => { await createUnit(id, tag.trim(), serial.trim() || undefined); const t = tag.trim(); setTag(""); setSerial(""); return `管理番号 ${t} を登録しました。`; }); }}>
            <div className="field"><TextField id="u-tag" label="管理番号（必須）" required maxLength={50} value={tag} onChange={setTag} /></div>
            <div className="field"><TextField id="u-serial" label="シリアル番号" maxLength={100} value={serial} onChange={setSerial} /></div>
            <Button fullWidth={false} type="submit" disabled={busy || !tag.trim()}>個体を登録する</Button>
          </form>
        </section>
      )}

      <section className="panel" aria-labelledby="arch-title">
        <h2 id="arch-title">この物品を廃止する</h2>
        <p className="hint">廃止すると一覧に出なくなり、借りられなくなります。履歴は残ります。貸出中のものがあるあいだは廃止できません。</p>
        <form onSubmit={(e) => { e.preventDefault(); if (!window.confirm(`「${it.name}」を廃止します。よろしいですか？`)) return; void act(async () => { await updateItem(id, { archived: true }); router.push("/admin/inventory"); return "廃止しました。"; }); }}>
          <Button variant="danger" fullWidth={false} type="submit" disabled={busy}>廃止する</Button>
        </form>
      </section>
    </main>
  );
}
