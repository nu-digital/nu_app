"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import Badge from "@/components/ui/Badge";
import Button from "@/components/ui/Button";
import Dialog from "@/components/ui/Dialog";
import { Loading } from "@/components/ui/States";
import TextField from "@/components/ui/TextField";
import { useToast } from "@/components/ui/Toast";
import { createCategory, deleteCategory, describe, listCategories, renameCategory, reorderCategories, type Category } from "@/lib/api";

export default function CategoriesPage() {
  return <AppShell>{() => <Categories />}</AppShell>;
}

type Dlg = { kind: "rename"; c: Category } | { kind: "delete"; c: Category } | null;

function Categories() {
  const toast = useToast();
  const [list, setList] = useState<Category[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [name, setName] = useState("");
  const [dlg, setDlg] = useState<Dlg>(null);
  const [text, setText] = useState("");
  const [dlgError, setDlgError] = useState("");

  const load = useCallback(async () => {
    try {
      setList((await listCategories()).items);
    } catch (e) {
      setError(describe(e));
    }
  }, []);
  useEffect(() => {
    let alive = true;
    listCategories().then((r) => alive && setList(r.items)).catch((e) => alive && setError(describe(e)));
    return () => { alive = false; };
  }, []);

  const run = async (f: () => Promise<string>) => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const done = await f();
      await load();
      toast("success", done);
      return true;
    } catch (e) {
      setError(describe(e));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const move = (i: number, to: number) => {
    if (!list || to < 0 || to >= list.length) return;
    const ids = list.map((c) => c.id);
    [ids[i], ids[to]] = [ids[to], ids[i]];
    void run(async () => { await reorderCategories(ids); return "並び順を変更しました。"; });
  };

  function open(d: Dlg) {
    setDlg(d);
    setDlgError("");
    setText(d?.kind === "rename" ? d.c.name : "");
  }

  async function submitDialog() {
    if (!dlg || busy) return;
    setBusy(true);
    setDlgError("");
    try {
      if (dlg.kind === "rename") { await renameCategory(dlg.c.id, text.trim()); toast("success", `「${text.trim()}」に変更しました。`); }
      else { await deleteCategory(dlg.c.id); toast("success", dlg.c.itemCount > 0 ? `「${dlg.c.name}」を削除し、${dlg.c.itemCount}件の物品を「その他」に移しました。` : `「${dlg.c.name}」を削除しました。`); }
      setDlg(null);
      await load();
    } catch (e) {
      setDlgError(describe(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="page">
      <p><Link href="/admin/inventory">← 物品の管理へ</Link></p>
      <h1>カテゴリの管理</h1>
      <p className="lead">物品の分類です。ここで追加・名前の変更・並び替え・削除ができます（管理者だけ）。並び順は、物品一覧のカテゴリの並びになります。削除したカテゴリの物品は「その他」に移ります。</p>
      {error && <Notice kind="error">{error}</Notice>}

      <section className="panel" aria-labelledby="add-title">
        <h2 id="add-title">カテゴリを追加する</h2>
        <form className="inline-form" onSubmit={async (e) => { e.preventDefault(); const n = name.trim(); if (await run(async () => { await createCategory(n); return `「${n}」を追加しました。`; })) setName(""); }}>
          <div className="field grow"><TextField id="cat-name" label="カテゴリ名" maxLength={30} value={name} onChange={(v: string) => setName(v)} /></div>
          <Button fullWidth={false} type="submit" disabled={busy || !name.trim()}>追加する</Button>
        </form>
      </section>

      <section aria-labelledby="list-title">
        <h2 id="list-title">カテゴリの一覧</h2>
        {!list ? <Loading /> : (
          <ul className="items" data-testid="category-list">
            {list.map((c, i) => (
              <li key={c.id} className="item" data-name={c.name}>
                <div className="grow"><strong>{c.name}</strong>{c.system && <> <Badge tone="neutral">削除できません</Badge></>}<div className="user-id">{c.itemCount}件の物品{c.system ? "（削除したカテゴリの物品の移し先）" : ""}</div></div>
                <div className="actions">
                  <Button variant="default" fullWidth={false} disabled={busy || i === 0} aria-label={`${c.name}を上へ`} onClick={() => move(i, i - 1)}>↑</Button>
                  <Button variant="default" fullWidth={false} disabled={busy || i === list.length - 1} aria-label={`${c.name}を下へ`} onClick={() => move(i, i + 1)}>↓</Button>
                  <Button variant="default" fullWidth={false} disabled={busy || c.system} onClick={() => open({ kind: "rename", c })}>名前を変える</Button>
                  <Button variant="danger" fullWidth={false} disabled={busy || c.system} onClick={() => open({ kind: "delete", c })}>削除</Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <Dialog
        open={dlg !== null}
        title={dlg?.kind === "rename" ? "カテゴリの名前を変える" : `「${dlg?.c.name ?? ""}」を削除する`}
        onClose={() => setDlg(null)}
        actions={<Button variant={dlg?.kind === "delete" ? "danger" : "primary"} fullWidth={false} disabled={busy || (dlg?.kind === "rename" && !text.trim())} onClick={submitDialog}>{dlg?.kind === "rename" ? "保存する" : "削除する"}</Button>}
      >
        {dlgError && <Notice kind="error">{dlgError}</Notice>}
        {dlg?.kind === "rename" && <div className="field"><TextField id="cat-rename" label="新しい名前" maxLength={30} value={text} onChange={(v: string) => setText(v)} /></div>}
        {dlg?.kind === "delete" && (dlg.c.itemCount > 0
          ? <p>このカテゴリの<strong>{dlg.c.itemCount}件</strong>の物品は、削除されずに「その他」へ移ります。</p>
          : <p>このカテゴリは使われていません。削除しても物品には影響しません。</p>)}
      </Dialog>
    </main>
  );
}
