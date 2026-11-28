"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import Badge from "@/components/ui/Badge";
import Button from "@/components/ui/Button";
import { AvatarStack } from "@/components/ui/Avatar";
import { CardSkeleton, EmptyState } from "@/components/ui/States";
import { describe, listContents, type ContentListItem } from "@/lib/api";

export default function ContentsPage() {
  return <AppShell>{() => <Contents />}</AppShell>;
}

const SECTION: Record<string, string> = { blog: "ブログ", news: "お知らせ", project: "作品" };
type Filters = { q: string; section: string; status: "" | "published" | "draft"; mine: boolean };
const INITIAL: Filters = { q: "", section: "", status: "", mine: false };

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
        <img src={c.coverUrl} alt="" loading="lazy" />
      ) : (
        <span className="thumb-empty" aria-hidden="true">{c.type === "project" ? "作品" : "記事"}</span>
      )}
      <span className="kind-tag">{SECTION[c.section]}</span>
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

  useEffect(() => {
    const t = setTimeout(() => setF((cur) => (cur.q === text.trim() ? cur : { ...cur, q: text.trim() })), 350);
    return () => clearTimeout(t);
  }, [text]);

  useEffect(() => {
    let alive = true;
    listContents({ q: f.q, section: f.section, status: f.status, mine: f.mine })
      .then((r) => { if (!alive) return; setItems(r.items); setNext(r.nextCursor); setError(""); setLoadedKey(key); })
      .catch((e) => { if (!alive) return; setError(describe(e)); setLoadedKey(key); });
    return () => { alive = false; };
  }, [f, key]);

  const set = useCallback((patch: Partial<Filters>) => setF((cur) => ({ ...cur, ...patch })), []);
  const filtered = f.q !== "" || f.section !== "" || f.status !== "" || f.mine;

  async function loadMore() {
    if (!next || more) return;
    setMore(true);
    try {
      const r = await listContents({ q: f.q, section: f.section, status: f.status, mine: f.mine, cursor: next });
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
        {[["", "すべて"], ["blog", "ブログ"], ["news", "お知らせ"], ["project", "作品"]].map(([v, l]) => (
          <button key={v} type="button" className="chip" aria-pressed={f.section === v} onClick={() => set({ section: v })}>{l}</button>
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
                  <p className="meta">{dateText(c)}</p>
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
              </li>
            );
          })}
        </ul>
      )}
      {next && <div className="center"><Button variant="default" fullWidth={false} disabled={more} onClick={loadMore}>{more ? "読み込んでいます…" : "さらに表示"}</Button></div>}
    </main>
  );
}
