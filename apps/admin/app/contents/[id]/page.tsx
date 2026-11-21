"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import AppShell from "@/components/AppShell";
import MarkdownPreview from "@/components/MarkdownPreview";
import Notice from "@/components/Notice";
import {
  conflictRevision, describe, getContent, publishContent, saveRevision, unpublishContent,
  type ContentDetail, type ContentRevision,
} from "@/lib/api";

export default function ContentEditorPage() {
  return <AppShell>{() => <Editor />}</AppShell>;
}

type Form = { title: string; summary: string; markdown: string; category: string; excerpt: string; dateLabel: string; dateKind: string; sortDate: string };

function fromRevision(r: ContentRevision): Form {
  return {
    title: r.title, summary: r.summary, markdown: r.markdown, category: r.metadata.category ?? "", excerpt: r.metadata.excerpt ?? "",
    dateLabel: r.date.label, dateKind: r.date.kind, sortDate: r.date.sortDate ?? "",
  };
}

const SECTION: Record<string, string> = { blog: "ブログ", news: "お知らせ", project: "作品" };

function Editor() {
  const { id } = useParams<{ id: string }>();
  const [doc, setDoc] = useState<ContentDetail | null>(null);
  const [form, setForm] = useState<Form | null>(null);
  const [baseRevision, setBaseRevision] = useState(0);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [conflict, setConflict] = useState<ContentRevision | null>(null);
  const [busy, setBusy] = useState(false);
  const [tab, setTab] = useState<"edit" | "preview">("edit");
  const [version, setVersion] = useState(0);
  const msgRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let alive = true;
    getContent(id)
      .then((d) => {
        if (!alive) return;
        setDoc(d);
        setForm(fromRevision(d.workingRevision));
        setBaseRevision(d.workingRevision.revisionNo);
      })
      .catch((e) => alive && setMsg({ kind: "error", text: (e as { status?: number }).status === 404 ? "この記事・作品は見つからないか、編集する権限がありません。" : describe(e) }));
    return () => { alive = false; };
  }, [id, version]);

  const saved = doc ? fromRevision(doc.workingRevision) : null;
  const dirty = !!(form && saved && JSON.stringify(form) !== JSON.stringify(saved));

  // 保存していない変更があるまま閉じようとしたら確認する
  useEffect(() => {
    if (!dirty) return;
    const h = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    window.addEventListener("beforeunload", h);
    return () => window.removeEventListener("beforeunload", h);
  }, [dirty]);

  if (!doc || !form) {
    return (
      <main className="page" aria-busy={!msg}>
        {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}
        <p><Link href="/contents">← 記事・作品へ</Link></p>
      </main>
    );
  }

  const set = (patch: Partial<Form>) => setForm({ ...form, ...patch });
  const notify = (m: { kind: "error" | "success"; text: string } | null) => {
    setMsg(m);
    if (m) setTimeout(() => msgRef.current?.focus(), 0);
  };

  async function onSave() {
    if (!doc || !form || busy) return;
    if (form.dateKind !== "unknown" && !form.sortDate) {
      notify({ kind: "error", text: "日付の種類が「不明」以外のときは、並び替え用の日付を入れてください。" });
      return;
    }
    setBusy(true);
    setConflict(null);
    try {
      const metadata: Record<string, string | null> = {};
      if (form.category.trim()) metadata.category = form.category.trim();
      if (form.excerpt.trim()) metadata.excerpt = form.excerpt.trim();
      const { content, changed } = await saveRevision(doc.id, {
        baseRevision, title: form.title, summary: form.summary, markdown: form.markdown, metadata,
        date: { label: form.dateLabel, kind: form.dateKind, sortDate: form.sortDate || null },
      });
      setDoc(content);
      setForm(fromRevision(content.workingRevision));
      setBaseRevision(content.workingRevision.revisionNo);
      notify({ kind: "success", text: changed ? `保存しました（版${content.workingRevision.revisionNo}）。` : "変更はありません。" });
    } catch (e) {
      const cur = conflictRevision(e);
      if (cur) {
        setConflict(cur);
        notify({ kind: "error", text: `他の人が先に保存しました（版${cur.revisionNo}・${cur.editedBy}さん）。どちらを残すか選んでください。` });
      } else {
        notify({ kind: "error", text: describe(e) });
      }
    } finally {
      setBusy(false);
    }
  }

  async function onPublish() {
    if (!doc || busy) return;
    const rev = doc.workingRevision.revisionNo;
    if (!window.confirm(`「${doc.workingRevision.title}」の版${rev}を公開します。公開サイトに表示されます。よろしいですか？`)) return;
    setBusy(true);
    try {
      const d = await publishContent(doc.id, rev);
      setDoc(d);
      notify({ kind: "success", text: `版${rev}を公開しました。` });
    } catch (e) {
      const cur = conflictRevision(e);
      if (cur) {
        notify({ kind: "error", text: `公開しようとした版が最新ではありません（最新は版${cur.revisionNo}）。最新を読み込んで確認してから公開してください。` });
        setVersion((v) => v + 1);
      } else {
        notify({ kind: "error", text: describe(e) });
      }
    } finally {
      setBusy(false);
    }
  }

  async function onUnpublish() {
    if (!doc || busy) return;
    if (!window.confirm("公開を止めます。公開サイトから見えなくなります（下書きは残ります）。よろしいですか？")) return;
    setBusy(true);
    try {
      setDoc(await unpublishContent(doc.id));
      notify({ kind: "success", text: "公開を止めました。" });
    } catch (e) {
      notify({ kind: "error", text: describe(e) });
    } finally {
      setBusy(false);
    }
  }

  const w = doc.workingRevision;
  const published = doc.publishedRevisionNo;
  return (
    <main className="page wide editor">
      <p><Link href="/contents">← 記事・作品へ</Link></p>
      <h1>{w.title}</h1>
      <p className="meta">
        {SECTION[doc.section]} ・ /{doc.section}/{doc.slug}/ ・ 作者 {doc.creator.displayName}さん ・ 作業中の版 {w.revisionNo}
        {published !== null ? ` ・ 公開中は版${published}` : " ・ 未公開"}
      </p>
      <div ref={msgRef} tabIndex={-1}>{msg && <Notice kind={msg.kind}>{msg.text}</Notice>}</div>
      {conflict && (
        <section className="panel" aria-label="保存の競合">
          <p>あなたの編集（版{baseRevision}から）と、{conflict.editedBy}さんが保存した最新の版{conflict.revisionNo}が食い違っています。</p>
          <div className="actions">
            <button className="btn secondary inline" onClick={() => { setConflict(null); setMsg(null); setVersion((v) => v + 1); }}>自分の編集を捨てて、最新を読み込む</button>
            <button className="btn inline" onClick={() => { setBaseRevision(conflict.revisionNo); setConflict(null); notify({ kind: "success", text: `最新の版${conflict.revisionNo}を元に、あなたの編集を保存できるようにしました。内容を確認して「保存する」を押してください（${conflict.editedBy}さんの変更は、あなたの内容で置き換わります）。` }); }}>自分の編集を残して、最新の上に保存し直す</button>
          </div>
        </section>
      )}

      <div className="tabs" role="tablist" aria-label="表示の切り替え">
        <button role="tab" aria-selected={tab === "edit"} className={`btn inline ${tab === "edit" ? "" : "secondary"}`} onClick={() => setTab("edit")}>編集</button>
        <button role="tab" aria-selected={tab === "preview"} className={`btn inline ${tab === "preview" ? "" : "secondary"}`} onClick={() => setTab("preview")}>プレビュー</button>
      </div>

      <div className={`editor-grid tab-${tab}`}>
        <section className="editor-edit" aria-label="編集">
          <div className="field">
            <label htmlFor="e-title">タイトル（必須）</label>
            <input id="e-title" className="input" maxLength={200} value={form.title} onChange={(e) => set({ title: e.target.value })} />
          </div>
          <div className="field">
            <label htmlFor="e-summary">要約</label>
            <textarea id="e-summary" className="input" rows={2} maxLength={500} value={form.summary} onChange={(e) => set({ summary: e.target.value })} />
          </div>
          <div className="field">
            <label htmlFor="e-md">本文（Markdown）</label>
            <textarea id="e-md" className="input md-input" rows={18} spellCheck={false} value={form.markdown} onChange={(e) => set({ markdown: e.target.value })} />
            <p className="hint">見出し・リスト・表・リンク・画像が使えます。生のHTMLは、折りたたみ（details・summary）と埋め込み用の限られた形だけです。YouTubeはURLを書くと動画が表示されます。</p>
          </div>
          <div className="field">
            <label htmlFor="e-cat">カテゴリ</label>
            <input id="e-cat" className="input" maxLength={30} value={form.category} onChange={(e) => set({ category: e.target.value })} />
          </div>
          <div className="field">
            <label htmlFor="e-excerpt">抜粋（一覧に出す短い紹介）</label>
            <textarea id="e-excerpt" className="input" rows={2} maxLength={300} value={form.excerpt} onChange={(e) => set({ excerpt: e.target.value })} />
          </div>
          <fieldset className="field">
            <legend>日付</legend>
            <label htmlFor="e-dkind">日付の種類</label>
            <select id="e-dkind" className="input" value={form.dateKind} onChange={(e) => set({ dateKind: e.target.value })}>
              <option value="day">はっきりした日（1日）</option>
              <option value="month">年月だけ</option>
              <option value="range">期間</option>
              <option value="unknown">不明</option>
            </select>
            <label htmlFor="e-dlabel">表示する日付（例: 2026年8月1〜2日）</label>
            <input id="e-dlabel" className="input" maxLength={100} value={form.dateLabel} onChange={(e) => set({ dateLabel: e.target.value })} />
            <label htmlFor="e-dsort">並び替え用の日付{form.dateKind === "unknown" ? "（任意）" : "（必須）"}</label>
            <input id="e-dsort" className="input" type="date" value={form.sortDate} onChange={(e) => set({ sortDate: e.target.value })} />
            <p className="hint">公開サイトには「表示する日付」がそのまま出ます。並び替え用の日付は並び順にだけ使い、開催日としては表示しません。</p>
          </fieldset>
        </section>
        <section className="editor-preview" aria-label="プレビュー">
          <h2>プレビュー</h2>
          <p className="hint">保存していない入力もそのまま表示します。危険な記述は表示の段階で取り除かれます。</p>
          <MarkdownPreview markdown={form.markdown} />
        </section>
      </div>

      <div className="actions sticky-actions">
        <button className="btn inline" onClick={onSave} disabled={busy || !dirty || !form.title.trim()}>保存する</button>
        {dirty ? <span className="hint">保存していない変更があります</span> : <span className="hint">保存済み</span>}
      </div>

      <section className="panel" aria-labelledby="pub-title">
        <h2 id="pub-title">公開</h2>
        {doc.canPublish ? (
          <>
            <p className="hint">{published === null ? "まだ公開していません。" : `公開中は版${published}です。${published !== w.revisionNo ? `作業中の版${w.revisionNo}は未公開です。` : "作業中の版と同じです。"}`}</p>
            <div className="actions">
              <button className="btn inline" onClick={onPublish} disabled={busy || dirty || published === w.revisionNo}>版{w.revisionNo}を公開する</button>
              {published !== null && <button className="btn secondary inline" onClick={onUnpublish} disabled={busy}>公開を止める</button>}
            </div>
            {dirty && <p className="hint">先に保存してください（保存した版を公開します）。</p>}
          </>
        ) : (
          <p className="hint">公開できるのは編集者（Editor）以上です。{published === null ? "公開してほしいときは、編集者に頼んでください。" : `公開中は版${published}です。編集を保存しても、編集者が公開し直すまでは公開サイトに反映されません。`}</p>
        )}
      </section>

      {doc.revisions && doc.revisions.length > 1 && (
        <section className="panel" aria-labelledby="hist-title">
          <h2 id="hist-title">保存の履歴</h2>
          <ul className="items">
            {doc.revisions.map((r) => (
              <li key={r.revisionNo} className="item">
                <div>版{r.revisionNo}{r.revisionNo === w.revisionNo ? "（作業中）" : ""}{r.revisionNo === published ? "（公開中）" : ""} ・ {r.title}<div className="user-id">{r.editedBy}さん ・ {new Date(r.createdAt).toLocaleString("ja-JP")}</div></div>
              </li>
            ))}
          </ul>
        </section>
      )}
    </main>
  );
}
