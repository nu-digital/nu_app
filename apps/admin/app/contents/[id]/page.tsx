"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import AppShell from "@/components/AppShell";
import MarkdownPreview from "@/components/MarkdownPreview";
import MembersPanel from "@/components/MembersPanel";
import Notice from "@/components/Notice";
import Badge from "@/components/ui/Badge";
import Button from "@/components/ui/Button";
import { Avatar } from "@/components/ui/Avatar";
import Dialog from "@/components/ui/Dialog";
import SegmentedControl from "@/components/ui/SegmentedControl";
import { Loading } from "@/components/ui/States";
import TextField from "@/components/ui/TextField";
import Textarea from "@/components/ui/Textarea";
import { useToast } from "@/components/ui/Toast";
import {
  conflictRevision, describe, getContent, publishContent, saveRevision, unpublishContent, uploadMedia,
  type ContentDetail, type ContentRevision, type MediaInfo,
} from "@/lib/api";
import { inputFromDate, labelFromDate, originalLabelNote, resolveDate } from "@/lib/content-date";
import { previewUrlTransform } from "@/lib/markdown-preview.mjs";

export default function ContentEditorPage() {
  return <AppShell>{() => <Editor />}</AppShell>;
}

/** 編集欄の状態。日付はユーザーには1つ（date）。内部の表示ラベル・種類は lib/content-date が保存時に決める。 */
type Form = { title: string; summary: string; markdown: string; category: string; excerpt: string; date: string; cover: MediaInfo | null };

function fromRevision(r: ContentRevision): Form {
  return { title: r.title, summary: r.summary, markdown: r.markdown, category: r.metadata.category ?? "", excerpt: r.metadata.excerpt ?? "", date: inputFromDate(r.date), cover: r.coverMedia };
}

const SECTION: Record<string, string> = { blog: "ブログ", news: "お知らせ", project: "作品" };
type Msg = { kind: "error" | "success"; text: string } | null;

function Editor() {
  const { id } = useParams<{ id: string }>();
  const toast = useToast();
  const [doc, setDoc] = useState<ContentDetail | null>(null);
  const [form, setForm] = useState<Form | null>(null);
  const [baseRevision, setBaseRevision] = useState(0);
  const [msg, setMsg] = useState<Msg>(null);
  const [conflict, setConflict] = useState<ContentRevision | null>(null);
  const [busy, setBusy] = useState(false);
  const [view, setView] = useState<"edit" | "preview">("edit");
  const [device, setDevice] = useState<"pc" | "sp">("pc");
  const [version, setVersion] = useState(0);
  const [confirm, setConfirm] = useState<"publish" | "unpublish" | null>(null);
  const msgRef = useRef<HTMLDivElement>(null);
  const mdRef = useRef<HTMLTextAreaElement>(null);
  const [uploads, setUploads] = useState<MediaInfo[]>([]);
  const [uploading, setUploading] = useState(false);

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
      <main className="page">
        {msg ? <Notice kind={msg.kind}>{msg.text}</Notice> : <Loading />}
        <p><Link href="/contents">← 記事・作品へ</Link></p>
      </main>
    );
  }

  const set = (patch: Partial<Form>) => setForm({ ...form, ...patch });
  const notifyError = (text: string) => {
    setMsg({ kind: "error", text });
    setTimeout(() => msgRef.current?.focus(), 0);
  };

  /** 保存する。成功したら最新の記事を返す（失敗・競合ならnull）。メンバーの保存からも使う。 */
  async function doSave(): Promise<ContentDetail | null> {
    if (!doc || !form) return null;
    setBusy(true);
    setConflict(null);
    setMsg(null);
    try {
      const metadata: Record<string, string | null> = {};
      if (form.category.trim()) metadata.category = form.category.trim();
      if (form.excerpt.trim()) metadata.excerpt = form.excerpt.trim();
      const { content, changed } = await saveRevision(doc.id, {
        baseRevision, title: form.title, summary: form.summary, markdown: form.markdown, metadata,
        date: resolveDate(doc.workingRevision.date, form.date),
        coverMediaId: form.cover?.id ?? null,
      });
      setDoc(content);
      setForm(fromRevision(content.workingRevision));
      setBaseRevision(content.workingRevision.revisionNo);
      toast("success", changed ? `保存しました（版${content.workingRevision.revisionNo}）。` : "変更はありません。");
      return content;
    } catch (e) {
      const cur = conflictRevision(e);
      if (cur) {
        setConflict(cur);
        notifyError(`他の人が先に保存しました（版${cur.revisionNo}・${cur.editedBy}さん）。どちらを残すか選んでください。`);
      } else {
        notifyError(describe(e));
      }
      return null;
    } finally {
      setBusy(false);
    }
  }
  const onSave = async () => { if (!busy) await doSave(); };

  async function onUpload(file: File | undefined) {
    if (!file || uploading) return;
    setUploading(true);
    setMsg(null);
    try {
      const m = await uploadMedia(file);
      setUploads((u) => [...u, m]);
      toast("success", `画像をアップロードしました（${m.width}×${m.height}）。「本文に挿入」か「カバーにする」で使えます。`);
    } catch (e) {
      notifyError(describe(e));
    } finally {
      setUploading(false);
    }
  }

  // 本文のカーソル位置に画像を挿入する（アップロードした画像のURLは、公開されるまで匿名には配信されない）
  function insertImage(m: MediaInfo) {
    if (!form) return;
    const el = mdRef.current;
    const at = el ? el.selectionStart : form.markdown.length;
    const text = `![画像の説明](${m.publicUrl})`;
    const before = form.markdown.slice(0, at), after = form.markdown.slice(at);
    const sep1 = before && !before.endsWith("\n\n") ? (before.endsWith("\n") ? "\n" : "\n\n") : "";
    const sep2 = after && !after.startsWith("\n\n") ? (after.startsWith("\n") ? "\n" : "\n\n") : "";
    set({ markdown: before + sep1 + text + sep2 + after });
  }

  async function onPublish() {
    if (!doc || busy) return;
    const rev = doc.workingRevision.revisionNo;
    setConfirm(null);
    setBusy(true);
    try {
      const d = await publishContent(doc.id, rev);
      setDoc(d);
      toast("success", `版${rev}を公開しました。`);
    } catch (e) {
      const cur = conflictRevision(e);
      if (cur) {
        notifyError(`公開しようとした版が最新ではありません（最新は版${cur.revisionNo}）。最新を読み込んで確認してから公開してください。`);
        setVersion((v) => v + 1);
      } else {
        notifyError(describe(e));
      }
    } finally {
      setBusy(false);
    }
  }

  async function onUnpublish() {
    if (!doc || busy) return;
    setConfirm(null);
    setBusy(true);
    try {
      setDoc(await unpublishContent(doc.id));
      toast("success", "公開を止めました。");
    } catch (e) {
      notifyError(describe(e));
    } finally {
      setBusy(false);
    }
  }

  const w = doc.workingRevision;
  const published = doc.publishedRevisionNo;
  const dateNote = originalLabelNote(w.date, form.date);
  const shownDate = form.date ? (form.date === inputFromDate(w.date) && w.date.label ? w.date.label : labelFromDate(form.date)) : "";
  const coverSrc = form.cover?.url ?? (w.metadata.image ? previewUrlTransform(w.metadata.image) : null);
  const people = [doc.creator, ...doc.collaborators.filter((c) => c.userId !== doc.creator.userId), ...(w.contributors ?? []).filter((c) => c.userId !== doc.creator.userId && !doc.collaborators.some((x) => x.userId === c.userId))];

  return (
    <main className="page wide editor">
      <p><Link href="/contents">← 記事・作品へ</Link></p>
      <div className="editor-head">
        <h1>{w.title}</h1>
        <p className="meta">
          <Badge tone="neutral" icon={false}>{SECTION[doc.section]}</Badge>{" "}
          {published !== null ? <Badge tone="success">公開中（版{published}）</Badge> : <Badge tone="neutral" icon="✎">未公開</Badge>}{" "}
          {published !== null && published !== w.revisionNo && <Badge tone="warning" icon="◐">未公開の変更あり</Badge>}
        </p>
        <p className="hint">公開URL: /{doc.section}/{doc.slug}/ ・ 作業中の版 {w.revisionNo}</p>
      </div>
      <div ref={msgRef} tabIndex={-1}>{msg && <Notice kind={msg.kind}>{msg.text}</Notice>}</div>
      {conflict && (
        <section className="panel" aria-label="保存の競合">
          <p>あなたの編集（版{baseRevision}から）と、{conflict.editedBy}さんが保存した最新の版{conflict.revisionNo}が食い違っています。</p>
          <div className="actions">
            <Button variant="default" fullWidth={false} onClick={() => { setConflict(null); setMsg(null); setVersion((v) => v + 1); }}>自分の編集を捨てて、最新を読み込む</Button>
            <Button fullWidth={false} onClick={() => { setBaseRevision(conflict.revisionNo); setConflict(null); setMsg(null); toast("success", `最新の版${conflict.revisionNo}を元に保存できるようにしました。内容を確認して「保存する」を押してください（${conflict.editedBy}さんの変更は、あなたの内容で置き換わります）。`); }}>自分の編集を残して、最新の上に保存し直す</Button>
          </div>
        </section>
      )}

      <div className="only-narrow">
        <SegmentedControl label="表示" value={view} onChange={setView} options={[{ value: "edit", label: "編集" }, { value: "preview", label: "プレビュー" }]} />
      </div>

      <div className={`editor-grid tab-${view}`}>
        <section className="editor-edit" aria-label="編集">
          <div className="ui-card">
            <h2>基本情報</h2>
            <div className="field">
              <TextField id="e-title" label="タイトル（必須）" maxLength={200} value={form.title} onChange={(value: string) => set({ title: value })} />
            </div>
            <div className="field">
              <label htmlFor="e-date">日付</label>
              <input id="e-date" className="input" type="date" value={form.date} onChange={(e) => set({ date: e.target.value })} />
              <p className="hint">
                {dateNote ? `公式サイトには「${dateNote}」と表示されています。日付を選び直すと、選んだ日付の表示に変わります。` : form.date ? "公式サイトでは、この日付の表示と並び順に使われます。" : "未設定です。選ぶと、公式サイトの表示と並び順に使われます。"}
              </p>
            </div>
            <Textarea id="e-summary" label="要約" rows={2} maxLength={500} value={form.summary} onChange={(v) => set({ summary: v })} />
            <details className="more">
              <summary>詳細設定（カテゴリ・抜粋）</summary>
              <div className="field"><TextField id="e-cat" label="カテゴリ" maxLength={30} value={form.category} onChange={(value: string) => set({ category: value })} /></div>
              <Textarea id="e-excerpt" label="抜粋（一覧に出す短い紹介）" rows={2} maxLength={300} value={form.excerpt} onChange={(v) => set({ excerpt: v })} />
            </details>
          </div>

          <div className="ui-card">
            <h2>本文</h2>
            <div className="field">
              <label htmlFor="e-md">本文（Markdown）</label>
              <textarea id="e-md" ref={mdRef} className="input md-input" rows={18} spellCheck={false} value={form.markdown} onChange={(e) => set({ markdown: e.target.value })} />
              <p className="hint">見出し・リスト・表・リンク・画像が使えます。生のHTMLは、折りたたみ（details・summary）と埋め込み用の限られた形だけです。YouTubeはURLを書くと動画が表示されます。</p>
            </div>
          </div>

          <fieldset className="ui-card field" aria-label="画像">
            <legend>画像</legend>
            <label htmlFor="e-file">画像をアップロードする（JPEG・PNG・WebP。10MiBまで）</label>
            <input id="e-file" type="file" accept="image/jpeg,image/png,image/webp" disabled={uploading} onChange={(e) => { void onUpload(e.target.files?.[0]); e.target.value = ""; }} />
            <p className="hint">アップロードした画像は、向きの補正と縮小をして、撮影場所などの情報（EXIF）を消して保存されます。記事で使うまで、他の人には公開されません。</p>
            {form.cover && (
              <div className="cover" data-testid="cover">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src={form.cover.url} alt="カバー画像" width={form.cover.width} height={form.cover.height} />
                <Button variant="default" fullWidth={false} onClick={() => set({ cover: null })}>カバー画像を外す</Button>
              </div>
            )}
            {!form.cover && w.metadata.image && <p className="hint">この記事のカバーは、公式サイトにある既存の画像です（プレビューに表示しています）。</p>}
            {uploads.length > 0 && (
              <ul className="items" data-testid="uploads">
                {uploads.map((m) => (
                  <li key={m.id} className="item">
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img className="thumb-sm" src={m.url} alt="アップロードした画像" width={64} height={64} />
                    <div className="actions">
                      <Button variant="default" fullWidth={false} onClick={() => insertImage(m)}>本文に挿入</Button>
                      <Button variant="default" fullWidth={false} onClick={() => set({ cover: m })}>カバーにする</Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </fieldset>
        </section>

        <section className="editor-preview" aria-label="プレビュー">
          <div className="preview-head">
            <h2>プレビュー</h2>
            <SegmentedControl label="プレビューの幅" value={device} onChange={setDevice} options={[{ value: "pc", label: "PC" }, { value: "sp", label: "スマホ" }]} />
          </div>
          <p className="hint">保存していない入力もそのまま表示します。公式サイトの表示に近づけていますが、細部（余白・書体）は異なります。危険な記述は表示の段階で取り除かれます。</p>
          <div className={`preview-frame device-${device}`}>
            {coverSrc && (
              // eslint-disable-next-line @next/next/no-img-element
              <img className="preview-cover" src={coverSrc} alt="カバー画像" />
            )}
            <div className="preview-title" role="presentation">{form.title || "（タイトル未入力）"}</div>
            <p className="meta">{[SECTION[doc.section], shownDate].filter(Boolean).join(" ・ ")}</p>
            <div className="preview-people">{people.slice(0, 6).map((p) => <span key={p.userId} className="preview-person"><Avatar name={p.displayName} mediaId={p.avatarMediaId} size={24} />{p.displayName}</span>)}</div>
            <MarkdownPreview markdown={form.markdown} />
          </div>
        </section>
      </div>

      <div className="actions sticky-actions">
        <Button fullWidth={false} onClick={onSave} disabled={busy || !dirty || !form.title.trim()}>{busy ? "保存しています…" : "保存する"}</Button>
        {doc.canPublish && (
          <Button variant="default" fullWidth={false} onClick={() => setConfirm("publish")} disabled={busy || dirty || published === w.revisionNo}>版{w.revisionNo}を公開する</Button>
        )}
        <span className="hint" aria-live="polite">{dirty ? "保存していない変更があります" : "保存済み"}</span>
      </div>

      <section className="panel" aria-labelledby="pub-title">
        <h2 id="pub-title">公開</h2>
        {doc.canPublish ? (
          <>
            <p className="hint">{published === null ? "まだ公開していません。" : `公開中は版${published}です。${published !== w.revisionNo ? `作業中の版${w.revisionNo}は未公開です。` : "作業中の版と同じです。"}`}</p>
            <div className="actions">
              <Button fullWidth={false} onClick={() => setConfirm("publish")} disabled={busy || dirty || published === w.revisionNo}>版{w.revisionNo}を公開する</Button>
              {published !== null && <Button variant="default" fullWidth={false} onClick={() => setConfirm("unpublish")} disabled={busy}>公開を止める</Button>}
            </div>
            {dirty && <p className="hint">先に保存してください（保存した版を公開します）。</p>}
          </>
        ) : (
          <p className="hint">公開できるのは編集者（Editor）以上です。{published === null ? "公開してほしいときは、編集者に頼んでください。" : `公開中は版${published}です。編集を保存しても、編集者が公開し直すまでは公式サイトに反映されません。`}</p>
        )}
      </section>

      <MembersPanel
        key={`${doc.id}:${w.revisionNo}:${doc.collaborators.map((c) => c.userId).join(",")}`}
        doc={doc}
        ensureSaved={async () => (dirty ? await doSave() : doc)}
        onSaved={(c) => { setDoc(c); setForm(fromRevision(c.workingRevision)); setBaseRevision(c.workingRevision.revisionNo); }}
        onConflict={() => setVersion((v) => v + 1)}
      />

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

      <Dialog
        open={confirm !== null}
        title={confirm === "publish" ? "公開しますか？" : "公開を止めますか？"}
        onClose={() => setConfirm(null)}
        actions={confirm === "publish"
          ? <Button fullWidth={false} onClick={onPublish}>公開する</Button>
          : <Button variant="danger" fullWidth={false} onClick={onUnpublish}>公開を止める</Button>}
      >
        {confirm === "publish"
          ? <p>「{w.title}」の版{w.revisionNo}を公開します。公式サイトに表示されます。</p>
          : <p>公式サイトから見えなくなります（下書きは残ります）。</p>}
      </Dialog>
    </main>
  );
}
