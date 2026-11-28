"use client";

import { previewUrlTransform } from "@/lib/markdown-preview.mjs";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import Badge from "@/components/ui/Badge";
import Button from "@/components/ui/Button";
import Dialog from "@/components/ui/Dialog";
import Menu from "@/components/ui/Menu";
import { useToast } from "@/components/ui/Toast";
import { AvatarStack } from "@/components/ui/Avatar";
import { CardSkeleton, EmptyState } from "@/components/ui/States";
import { deleteContent, describe, listContents, type ContentListItem } from "@/lib/api";

export default function ContentsPage() {
  return <AppShell>{() => <Contents />}</AppShell>;
}

const SECTION: Record<string, string> = { blog: "ブログ", news: "お知らせ", project: "作品" };
const KIND: Record<ContentListItem["type"], string> = { article: "記事", project: "作品" };
/** 記事と作品は混在して新しい順に並べる。type で絞り込む（すべて／記事／作品）。 */
type Filters = { q: string; type: "" | "article" | "project"; status: "" | "published" | "draft"; mine: boolean };
const INITIAL: Filters = { q: "", type: "", status: "", mine: false };

/** 状態: 公開中／下書き＋公開後の変更あり。色・記号・文字で区別する。 */
export function ContentStatus({ c }: { c: Pick<ContentListItem, "isPublished" | "hasUnpublishedChanges"> }) {
  return (
    <span className="item-status">
      {c.isPublished ? <Badge tone="success">公開中</Badge> : <Badge tone="neutral" icon="✎">下書き</Badge>}
      {c.hasUnpublishedChanges && <Badge tone="warning" icon="◐">未公開の変更あり</Badge>}
    </span>
  );
}

function dateText(c: ContentListItem): string {
  return c.dateLabel || `${new Date(c.updatedAt).toLocaleDateString("ja-JP")}に更新`;
}

function Thumb({ c }: { c: ContentListItem }) {
  return (
    <div className={`thumb thumb-${c.section}`}>
      {c.coverUrl ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={previewUrlTransform(c.coverUrl)} alt="" loading="lazy" />
      ) : (
        <span className="thumb-empty" aria-hidden="true">{c.type === "project" ? "作品" : "記事"}</span>
      )}
      <span className={`kind-tag kind-${c.type}`}>{KIND[c.type]}</span>
    </div>
  );
}

function Contents() {
  const [items, setItems] = useState<ContentListItem[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [text, setText] = useState("");
  const [f, setF] = useState<Filters>(INITIAL);
  const [loadedKey, setLoadedKey] = useState("");
  const [more, setMore] = useState(false);
  const [error, setError] = useState("");
  const key = JSON.stringify(f);
  const loading = loadedKey !== key;
  const router = useRouter();
  const toast = useToast();
  const [del, setDel] = useState<ContentListItem | null>(null);
  const [delBusy, setDelBusy] = useState(false);

  async function confirmDelete() {
    if (!del || delBusy) return;
    setDelBusy(true);
    try {
      await deleteContent(del.id);
      toast("success", `「${del.title}」を削除しました。`);
      setItems((cur) => cur.filter((x) => x.id !== del.id));
      setDel(null);
    } catch (e) {
      setError(describe(e));
      setDel(null);
    } finally {
      setDelBusy(false);
    }
  }

  useEffect(() => {
    const t = setTimeout(() => setF((cur) => (cur.q === text.trim() ? cur : { ...cur, q: text.trim() })), 350);
    return () => clearTimeout(t);
  }, [text]);

  useEffect(() => {
    let alive = true;
    listContents({ q: f.q, type: f.type, status: f.status, mine: f.mine })
      .then((r) => { if (!alive) return; setItems(r.items); setNext(r.nextCursor); setError(""); setLoadedKey(key); })
      .catch((e) => { if (!alive) return; setError(describe(e)); setLoadedKey(key); });
    return () => { alive = false; };
  }, [f, key]);

  const set = useCallback((patch: Partial<Filters>) => setF((cur) => ({ ...cur, ...patch })), []);
  const filtered = f.q !== "" || f.type !== "" || f.status !== "" || f.mine;

  async function loadMore() {
    if (!next || more) return;
    setMore(true);
    try {
      const r = await listContents({ q: f.q, type: f.type, status: f.status, mine: f.mine, cursor: next });
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
        <h1>記事・作品</h1>
        <nav className="subnav"><Button fullWidth={false} href="/contents/new">新しく書く</Button></nav>
      </div>
      <p className="lead">活動報告や作品紹介を書きます。公開できるのは編集者（Editor）以上です。公開すると公式サイトに表示されます。</p>
      {error && <Notice kind="error">{error}</Notice>}
      <div className="toolbar" role="search">
        <div className="field search-field">
          <label htmlFor="cq" className="visually-hidden">タイトルで探す</label>
          <input id="cq" className="input" type="search" placeholder="タイトルで探す" value={text} onChange={(e) => setText(e.target.value)} />
        </div>
      </div>
      <div className="chips" role="group" aria-label="絞り込み">
        {([["", "すべて"], ["article", "記事"], ["project", "作品"]] as const).map(([v, l]) => (
          <button key={v} type="button" className="chip" aria-pressed={f.type === v} onClick={() => set({ type: v })}>{l}</button>
        ))}
        <span className="chip-sep" aria-hidden="true" />
        <button type="button" className="chip" aria-pressed={f.status === "published"} onClick={() => set({ status: f.status === "published" ? "" : "published" })}>公開中</button>
        <button type="button" className="chip" aria-pressed={f.status === "draft"} onClick={() => set({ status: f.status === "draft" ? "" : "draft" })}>下書き</button>
        <button type="button" className="chip" aria-pressed={f.mine} onClick={() => set({ mine: !f.mine })}>自分が関わっているもの</button>
      </div>
      <p className="result-count" aria-live="polite">{loading ? "" : `${items.length}${next ? "件以上" : "件"}`}</p>
      {loading ? <CardSkeleton /> : items.length === 0 ? (
        <EmptyState title={filtered ? "条件に合う記事・作品はありません" : "まだ記事・作品がありません"} action={filtered ? <Button variant="default" fullWidth={false} onClick={() => { setText(""); setF(INITIAL); }}>条件をリセット</Button> : <Button fullWidth={false} href="/contents/new">新しく書く</Button>}>
          {filtered ? "検索語や絞り込みを変えてみてください。" : "最初の1本を書いてみましょう。"}
        </EmptyState>
      ) : (
        <ul className="grid content-grid">
          {items.map((c) => {
            const body = (
              <>
                <Thumb c={c} />
                <div className="item-body">
                  <h2>{c.title}</h2>
                  <p className="meta">{[c.type === "article" ? SECTION[c.section] : "", dateText(c)].filter(Boolean).join(" ・ ")}</p>
                  <ContentStatus c={c} />
                </div>
              </>
            );
            return (
              <li key={c.id} data-slug={c.slug}>
                {c.canEdit ? (
                  <Link className="item-card" href={`/contents/${c.id}`} data-title={c.title}>{body}</Link>
                ) : (
                  <div className="item-card static" data-title={c.title}>{body}</div>
                )}
                {/* メンバーの重ね表示は、リンクの外に置く（クリックで名前の一覧を開くため） */}
                <div className="card-people"><AvatarStack people={c.members} />{!c.canEdit && <span className="hint">閲覧のみ</span>}</div>
                {c.canEdit && (
                  <div className="card-menu">
                    <Menu label={`「${c.title}」のメニュー`} items={[
                      { key: "edit", label: "編集する", onSelect: () => router.push(`/contents/${c.id}`) },
                      ...(c.canDelete ? [{ key: "delete", label: "削除する", danger: true, onSelect: () => setDel(c) }] : []),
                    ]} />
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <Dialog
        open={del !== null}
        title="削除しますか？"
        onClose={() => setDel(null)}
        actions={<Button variant="danger" fullWidth={false} disabled={delBusy} onClick={confirmDelete}>削除する</Button>}
      >
        {del && (del.isPublished
          ? <p>「{del.title}」を削除します。<strong>公開中の{KIND[del.type]}なので、公式サイトからも見えなくなります。</strong>一覧からも消えます。</p>
          : <p>「{del.title}」を削除します。一覧から消えます。</p>)}
      </Dialog>
      {next && <div className="center"><Button variant="default" fullWidth={false} disabled={more} onClick={loadMore}>{more ? "読み込んでいます…" : "さらに表示"}</Button></div>}
    </main>
  );
}
