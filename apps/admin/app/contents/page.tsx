"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import { describe, listContents, type ContentListItem } from "@/lib/api";

export default function ContentsPage() {
  return <AppShell>{() => <Contents />}</AppShell>;
}

const SECTION: Record<string, string> = { blog: "ブログ", news: "お知らせ", project: "作品" };

function Contents() {
  const [items, setItems] = useState<ContentListItem[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [section, setSection] = useState("");
  const [mine, setMine] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    listContents({ section, mine })
      .then((r) => { if (!alive) return; setItems(r.items); setNext(r.nextCursor); setError(""); setLoading(false); })
      .catch((e) => { if (!alive) return; setError(describe(e)); setLoading(false); });
    return () => { alive = false; };
  }, [section, mine]);

  return (
    <main className="page wide">
      <h1>記事・作品</h1>
      <p className="lead">活動報告や作品紹介の下書きを書きます。公開できるのは編集者（Editor）以上です。公開すると、公開サイトに表示されます。</p>
      <div className="subnav">
        <Button fullWidth={false} href="/contents/new">新しく書く</Button>
      </div>
      {error && <Notice kind="error">{error}</Notice>}
      <div className="filters">
        <div className="field">
          <label htmlFor="c-section">区分</label>
          <select id="c-section" className="input" value={section} onChange={(e) => { setLoading(true); setSection(e.target.value); }}>
            <option value="">すべて</option>
            <option value="blog">ブログ</option>
            <option value="news">お知らせ</option>
            <option value="project">作品</option>
          </select>
        </div>
        <div className="field">
          <label><input type="checkbox" checked={mine} onChange={(e) => { setLoading(true); setMine(e.target.checked); }} /> 自分が書いている・共同編集しているものだけ</label>
        </div>
      </div>
      {loading ? <p aria-busy="true">読み込んでいます…</p> : items.length === 0 ? <p>該当する記事・作品はありません。</p> : (
        <ul className="items">
          {items.map((c) => (
            <li key={c.id} className="item" data-slug={c.slug}>
              <div>
                {c.canEdit ? <Link href={`/contents/${c.id}`}><strong>{c.title}</strong></Link> : <strong>{c.title}</strong>}{" "}
                {c.isPublished ? <span className="badge status-active">公開中</span> : <span className="badge status-invited">下書き</span>}{" "}
                {c.hasUnpublishedChanges && <span className="badge status-disabled">公開後の変更あり（未公開）</span>}
                <div className="user-id">{SECTION[c.section]} ・ {c.slug} ・ 版{c.revisionNo} ・ {new Date(c.updatedAt).toLocaleString("ja-JP")}</div>
                {c.summary && <div className="meta">{c.summary}</div>}
                {!c.canEdit && <div className="user-id">公開されている記事です（編集できるのは作者・共同編集者・編集者です）</div>}
              </div>
            </li>
          ))}
        </ul>
      )}
      {next && <Button variant="default" fullWidth={false} type="submit" onClick={async () => { try { const r = await listContents({ section, mine, cursor: next }); setItems((c) => [...c, ...r.items]); setNext(r.nextCursor); } catch (e) { setError(describe(e)); } }}>さらに表示</Button>}
    </main>
  );
}
