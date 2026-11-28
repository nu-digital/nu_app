"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

import AppShell from "@/components/AppShell";
import { MAX_IMAGE_BYTES, imageFiles, useBodyImages } from "@/components/editor/useBodyImages";
import MarkdownPreview from "@/components/MarkdownPreview";
import MembersPanel from "@/components/MembersPanel";
import Notice from "@/components/Notice";
import Badge from "@/components/ui/Badge";
import Button from "@/components/ui/Button";
import { Avatar, AvatarStack } from "@/components/ui/Avatar";
import Dialog from "@/components/ui/Dialog";
import Menu, { type MenuItem } from "@/components/ui/Menu";
import SegmentedControl from "@/components/ui/SegmentedControl";
import { Loading } from "@/components/ui/States";
import TextField from "@/components/ui/TextField";
import Textarea from "@/components/ui/Textarea";
import { useToast } from "@/components/ui/Toast";
import {
  conflictRevision, deleteContent, describe, getContent, publishContent, saveRevision, unpublishContent, uploadMedia,
  type ContentDetail, type ContentRevision, type MediaInfo,
} from "@/lib/api";
import { inputFromDate, labelFromDate, originalLabelNote, resolveDate } from "@/lib/content-date";
import { hasPlaceholder } from "@/lib/markdown-insert.mjs";
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
type Confirm = "publish" | "unpublish" | "delete" | null;

const sizeText = (b: number) => (b >= 1024 * 1024 ? `${(b / 1024 / 1024).toFixed(1)}MB` : `${Math.max(1, Math.round(b / 1024))}KB`);
const draftKey = (id: string) => `nu.editor-draft.${id}`;
const readDraft = (id: string): { form: Form; at: number } | null => {
  try { const v = localStorage.getItem(draftKey(id)); return v ? (JSON.parse(v) as { form: Form; at: number }) : null; } catch { return null; }
};
const writeDraft = (id: string, form: Form | null) => {
  try { if (form) localStorage.setItem(draftKey(id), JSON.stringify({ form, at: Date.now() })); else localStorage.removeItem(draftKey(id)); } catch { /* 保存できなくても編集は続けられる */ }
};

function Editor() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
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
  const [confirm, setConfirm] = useState<Confirm>(null);
  // 設定欄は本文の広さを優先して、既定ではたたむ（開閉はブラウザーに覚える）
  const [metaOpen, setMetaOpenState] = useState(() => { try { return localStorage.getItem("nu.editor-meta-open") === "1"; } catch { return false; } });
  const setMetaOpen = (open: boolean) => { setMetaOpenState(open); try { localStorage.setItem("nu.editor-meta-open", open ? "1" : "0"); } catch { /* 覚えられなくてもよい */ } };
  const [dragging, setDragging] = useState(false);
  const [restore, setRestore] = useState<Form | null>(null);
  const [coverBusy, setCoverBusy] = useState<number | null>(null);
  const msgRef = useRef<HTMLDivElement>(null);
  const mdRef = useRef<HTMLTextAreaElement>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const coverFileRef = useRef<HTMLInputElement>(null);
  const syncing = useRef(false);

  useEffect(() => {
    let alive = true;
    getContent(id)
      .then((d) => {
        if (!alive) return;
        setDoc(d);
        const f = fromRevision(d.workingRevision);
        setForm(f);
        setBaseRevision(d.workingRevision.revisionNo);
        // 前回、保存しないまま閉じた編集内容が残っていれば、復元を案内する
        const dr = readDraft(d.id);
        if (dr && JSON.stringify(dr.form) !== JSON.stringify(f)) setRestore(dr.form);
        else writeDraft(d.id, null);
      })
      .catch((e) => alive && setMsg({ kind: "error", text: (e as { status?: number }).status === 404 ? "この記事・作品は見つからないか、編集する権限がありません。" : describe(e) }));
    return () => { alive = false; };
  }, [id, version]);

  const updateMarkdown = useCallback((fn: (md: string) => string) => setForm((f) => (f ? { ...f, markdown: fn(f.markdown) } : f)), []);
  const images = useBodyImages({ textarea: mdRef, getText: () => mdRef.current?.value ?? "", update: updateMarkdown });

  const saved = doc ? fromRevision(doc.workingRevision) : null;
  const dirty = !!(form && saved && JSON.stringify(form) !== JSON.stringify(saved));

  // 保存していない編集内容をブラウザーにも控えておく（誤って閉じても戻せる）。保存すると消す。
  useEffect(() => {
    if (!doc || !form || restore) return;
    if (!dirty) return;
    const t = setTimeout(() => writeDraft(doc.id, form), 800);
    return () => clearTimeout(t);
  }, [doc, form, dirty, restore]);

  // 保存していない変更があるまま閉じる・サイト内のリンクで移るときは確認する
  useEffect(() => {
    if (!dirty && !images.busy) return;
    const before = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    const click = (e: MouseEvent) => {
      const a = (e.target as Element | null)?.closest?.("a[href]");
      const href = a?.getAttribute("href") ?? "";
      if (!a || !href.startsWith("/") || a.getAttribute("target") === "_blank") return;
      if (!window.confirm("保存していない変更があります。このまま移動すると、変更は失われます。移動しますか？")) { e.preventDefault(); e.stopPropagation(); }
    };
    window.addEventListener("beforeunload", before);
    document.addEventListener("click", click, true);
    return () => { window.removeEventListener("beforeunload", before); document.removeEventListener("click", click, true); };
  }, [dirty, images.busy]);

  if (!doc || !form) {
    return (
      <main className="page">
        {msg ? <Notice kind={msg.kind}>{msg.text}</Notice> : <Loading />}
        <p><Link href="/contents">← 記事・作品へ</Link></p>
      </main>
    );
  }

  const set = (patch: Partial<Form>) => setForm((f) => (f ? { ...f, ...patch } : f));
  const notifyError = (text: string) => {
    setMsg({ kind: "error", text });
    setTimeout(() => msgRef.current?.focus(), 0);
  };
  const w = doc.workingRevision;
  const published = doc.publishedRevisionNo;
  const hasUpdate = published !== null && published !== w.revisionNo;
  const nothingToPublish = published !== null && !hasUpdate && !dirty;

  /** 下書きとして保存する。成功したら最新の記事を返す（失敗・競合ならnull）。メンバーの保存・公開からも使う。 */
  async function doSave(): Promise<ContentDetail | null> {
    if (!doc || !form) return null;
    setBusy(true);
    setConflict(null);
    setMsg(null);
    try {
      const metadata: Record<string, string | null> = {};
      if (form.category.trim()) metadata.category = form.category.trim();
      if (form.excerpt.trim()) metadata.excerpt = form.excerpt.trim();
      const { content } = await saveRevision(doc.id, {
        baseRevision, title: form.title, summary: form.summary, markdown: form.markdown, metadata,
        date: resolveDate(doc.workingRevision.date, form.date),
        coverMediaId: form.cover?.id ?? null,
      });
      setDoc(content);
      setForm(fromRevision(content.workingRevision));
      setBaseRevision(content.workingRevision.revisionNo);
      writeDraft(doc.id, null);
      return content;
    } catch (e) {
      const cur = conflictRevision(e);
      if (cur) {
        setConflict(cur);
        notifyError(`${cur.editedBy}さんが先に保存しました。どちらの内容を残すか選んでください。`);
      } else {
        notifyError(describe(e));
      }
      return null;
    } finally {
      setBusy(false);
    }
  }

  async function onSaveDraft() {
    if (busy) return;
    const c = await doSave();
    if (c) toast("success", published !== null ? "下書きを保存しました。公開中のページは変わりません。" : "下書きを保存しました。");
  }

  async function onPublish() {
    if (!doc || busy) return;
    setConfirm(null);
    const c = dirty ? await doSave() : doc; // 編集中の内容があれば、先に保存してから公開する
    if (!c) return;
    setBusy(true);
    try {
      const d = await publishContent(c.id, c.workingRevision.revisionNo);
      setDoc(d);
      toast("success", published !== null ? "公開内容を更新しました。" : "公開しました。公式サイトに表示されます。");
    } catch (e) {
      const cur = conflictRevision(e);
      if (cur) {
        notifyError("他の人が先に保存したため、公開できませんでした。最新の内容を読み込みます。確認してからもう一度公開してください。");
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
      toast("success", "公開を止めました。下書きとして残っています。");
    } catch (e) {
      notifyError(describe(e));
    } finally {
      setBusy(false);
    }
  }

  async function onDelete() {
    if (!doc || busy) return;
    setConfirm(null);
    setBusy(true);
    try {
      await deleteContent(doc.id);
      writeDraft(doc.id, null);
      toast("success", `「${w.title}」を削除しました。`);
      router.push("/contents");
    } catch (e) {
      notifyError(describe(e));
      setBusy(false);
    }
  }

  // カバー画像: ドラッグ＆ドロップ・クリックで選ぶ
  async function setCoverFrom(list: File[]) {
    const f = list[0];
    if (!f || coverBusy !== null) return;
    if (f.size > MAX_IMAGE_BYTES) { notifyError("10MiBを超える画像はアップロードできません。"); return; }
    setCoverBusy(0);
    try {
      const m = await uploadMedia(f, "cms", (r) => setCoverBusy(r));
      set({ cover: m });
    } catch (e) {
      notifyError(describe(e));
    } finally {
      setCoverBusy(null);
    }
  }

  // 本文とプレビューのスクロールをそろえる（割合で追う）
  const sync = (from: HTMLElement | null, to: HTMLElement | null) => {
    if (!from || !to || syncing.current) return;
    const max = from.scrollHeight - from.clientHeight;
    if (max <= 0) return;
    syncing.current = true;
    to.scrollTop = (from.scrollTop / max) * (to.scrollHeight - to.clientHeight);
    requestAnimationFrame(() => { syncing.current = false; });
  };

  const dateNote = originalLabelNote(w.date, form.date);
  const shownDate = form.date ? (form.date === inputFromDate(w.date) && w.date.label ? w.date.label : labelFromDate(form.date)) : "";
  const coverSrc = form.cover?.url ?? (w.metadata.image ? previewUrlTransform(w.metadata.image) : null);
  const people = [doc.creator, ...doc.collaborators.filter((c) => c.userId !== doc.creator.userId), ...(w.contributors ?? []).filter((c) => c.userId !== doc.creator.userId && !doc.collaborators.some((x) => x.userId === c.userId))];
  const uploadingNow = images.busy || hasPlaceholder(form.markdown);

  const menu: MenuItem[] = [
    ...(doc.canPublish && published !== null ? [{ key: "unpublish", label: "公開を止める", onSelect: () => setConfirm("unpublish") }] : []),
    ...(doc.canDelete ? [{ key: "delete", label: "削除する", danger: true, onSelect: () => setConfirm("delete") }] : []),
  ];

  return (
    <main className="editor-page">
      <h1 className="visually-hidden">{w.title}</h1>
      <header className="editor-bar">
        <Link href="/contents" className="back-link">← 記事・作品</Link>
        <div className="editor-state" aria-live="polite">
          {published === null ? <Badge tone="neutral" icon="✎">下書き</Badge> : hasUpdate ? <Badge tone="warning" icon="◐">公開中・更新あり</Badge> : <Badge tone="success">公開中</Badge>}
          <span className="hint">{uploadingNow ? "画像をアップロードしています…" : busy ? "保存しています…" : dirty ? "保存していない変更があります" : "保存済み"}</span>
        </div>
        <div className="editor-actions">
          <Button variant="default" fullWidth={false} onClick={onSaveDraft} disabled={busy || !dirty || !form.title.trim() || uploadingNow}>下書き保存</Button>
          {doc.canPublish && (
            <Button fullWidth={false} onClick={() => setConfirm("publish")} disabled={busy || nothingToPublish || !form.title.trim() || uploadingNow}>{published === null ? "公開する" : "公開内容を更新"}</Button>
          )}
          <Menu label="その他の操作" items={menu} />
        </div>
      </header>

      <div ref={msgRef} tabIndex={-1}>{msg && <Notice kind={msg.kind}>{msg.text}</Notice>}</div>
      {restore && (
        <section className="panel" aria-label="編集内容の復元">
          <p>前回、保存しないまま閉じた編集内容がブラウザーに残っています。復元しますか？</p>
          <div className="actions">
            <Button fullWidth={false} onClick={() => { setForm(restore); setRestore(null); }}>復元する</Button>
            <Button variant="default" fullWidth={false} onClick={() => { writeDraft(doc.id, null); setRestore(null); }}>破棄する</Button>
          </div>
        </section>
      )}
      {conflict && (
        <section className="panel" aria-label="保存の競合">
          <p>{conflict.editedBy}さんが先に保存しました。あなたの編集内容と食い違っています。</p>
          <div className="actions">
            <Button variant="default" fullWidth={false} onClick={() => { setConflict(null); setMsg(null); setVersion((v) => v + 1); }}>自分の編集を捨てて、最新を読み込む</Button>
            <Button fullWidth={false} onClick={() => { setBaseRevision(conflict.revisionNo); setConflict(null); setMsg(null); toast("success", `最新の内容の上に保存できるようにしました。確認して「下書き保存」を押してください（${conflict.editedBy}さんの変更は、あなたの内容で置き換わります）。`); }}>自分の編集を残して、最新の上に保存し直す</Button>
          </div>
        </section>
      )}
      {!doc.canPublish && <p className="hint editor-note">公開できるのは編集者（Editor）以上です。{published === null ? "公開してほしいときは、編集者に頼んでください。" : "編集は下書きとして保存され、編集者が公開し直すまで公式サイトは変わりません。"}</p>}

      {/* ---- 上: メタ情報（タイトル・カバー・日付・メンバー・要約）---- */}
      <section className={`editor-meta${metaOpen ? "" : " closed"}`} aria-label="記事の情報">
        <div className="meta-head">
          <div className="field grow meta-title">
            <TextField id="e-title" label="タイトル（必須）" maxLength={200} value={form.title} onChange={(value: string) => set({ title: value })} />
          </div>
          <Button variant="default" fullWidth={false} aria-expanded={metaOpen} onClick={() => setMetaOpen(!metaOpen)}>{metaOpen ? "設定をたたむ" : "設定を開く"}</Button>
        </div>
        {metaOpen ? (
          <div className="meta-body">
            <div className="meta-cover">
              <div
                className={`cover-drop${coverBusy !== null ? " busy" : ""}`}
                data-testid="cover-drop"
                onDragOver={(e) => { if (e.dataTransfer.types.includes("Files")) e.preventDefault(); }}
                onDrop={(e) => { e.preventDefault(); void setCoverFrom(imageFiles(e.dataTransfer.files)); }}
              >
                {coverSrc ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={coverSrc} alt="カバー画像" data-testid="cover" />
                ) : <span className="cover-empty">カバー画像</span>}
                <button type="button" className="cover-pick" disabled={coverBusy !== null} onClick={() => coverFileRef.current?.click()}>
                  {coverBusy !== null ? `アップロード中 ${Math.round(coverBusy * 100)}%` : coverSrc ? "変更する（ドロップでも可）" : "画像をドロップ、またはクリックして選ぶ"}
                </button>
                <input ref={coverFileRef} type="file" accept="image/jpeg,image/png,image/webp" className="file-hidden" aria-label="カバー画像を選ぶ" onChange={(e) => { void setCoverFrom(imageFiles(e.target.files)); e.target.value = ""; }} />
              </div>
              {form.cover && <Button variant="default" fullWidth={false} onClick={() => set({ cover: null })}>カバー画像を外す</Button>}
              {!form.cover && w.metadata.image && <p className="hint">いまのカバーは公式サイトにある既存の画像です。</p>}
            </div>
            <div className="meta-fields">
              <div className="field">
                <label htmlFor="e-date">日付</label>
                <input id="e-date" className="input" type="date" value={form.date} onChange={(e) => set({ date: e.target.value })} />
                <p className="hint">
                  {dateNote ? `公式サイトには「${dateNote}」と表示されています。日付を選び直すと、選んだ日付の表示に変わります。` : form.date ? "公式サイトの表示と並び順に使われます。" : "未設定です。選ぶと、公式サイトの表示と並び順に使われます。"}
                </p>
              </div>
              <Textarea id="e-summary" label="要約" rows={2} maxLength={500} value={form.summary} onChange={(v) => set({ summary: v })} />
              <details className="more">
                <summary>詳細設定（カテゴリ・抜粋）</summary>
                <div className="field"><TextField id="e-cat" label="カテゴリ" maxLength={30} value={form.category} onChange={(value: string) => set({ category: value })} /></div>
                <Textarea id="e-excerpt" label="抜粋（一覧に出す短い紹介）" rows={2} maxLength={300} value={form.excerpt} onChange={(v) => set({ excerpt: v })} />
              </details>
              <p className="hint">公開されるURL: /{doc.section}/{doc.slug}/</p>
            </div>
            <details className="meta-members">
              <summary>メンバー（{people.length}人）：{people.map((p) => p.displayName).join("、")}</summary>
              <MembersPanel
                key={`${doc.id}:${w.revisionNo}:${doc.collaborators.map((c) => c.userId).join(",")}`}
                doc={doc}
                ensureSaved={async () => (dirty ? await doSave() : doc)}
                onSaved={(c) => { setDoc(c); setForm(fromRevision(c.workingRevision)); setBaseRevision(c.workingRevision.revisionNo); }}
                onConflict={() => setVersion((v) => v + 1)}
              />
            </details>
          </div>
        ) : (
          <div className="meta-collapsed" data-testid="meta-collapsed">
            {coverSrc ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img className="mini-cover" src={coverSrc} alt="カバー画像" />
            ) : <span className="mini-cover empty" aria-label="カバー画像なし">なし</span>}
            <span className="mini-date">{[SECTION[doc.section], shownDate || "日付未設定"].join(" ・ ")}</span>
            <AvatarStack people={people.map((p) => ({ userId: p.userId, displayName: p.displayName, avatarMediaId: p.avatarMediaId }))} />
            {form.summary && <span className="hint mini-summary">{form.summary}</span>}
          </div>
        )}
      </section>

      <div className="only-narrow editor-tabs">
        <SegmentedControl label="表示" value={view} onChange={setView} options={[{ value: "edit", label: "編集" }, { value: "preview", label: "プレビュー" }]} />
      </div>

      {/* ---- 下: 本文（左）とプレビュー（右）---- */}
      <div className={`editor-work tab-${view}`}>
        <section className="editor-write" aria-label="本文">
          <div className="write-bar">
            <label htmlFor="e-md" className="write-title">本文（Markdown）</label>
            <Button variant="default" fullWidth={false} onClick={() => fileRef.current?.click()}>画像を追加</Button>
            <input ref={fileRef} type="file" multiple accept="image/jpeg,image/png,image/webp" className="file-hidden" aria-label="本文に画像を追加" onChange={(e) => { images.addFiles(imageFiles(e.target.files)); e.target.value = ""; }} />
          </div>
          <textarea
            id="e-md"
            ref={mdRef}
            className={`input md-input${dragging ? " dragging" : ""}`}
            spellCheck={false}
            value={form.markdown}
            placeholder="ここに本文を書きます。画像はドラッグ＆ドロップか貼り付けで入れられます。"
            onChange={(e) => set({ markdown: e.target.value })}
            onScroll={(e) => sync(e.currentTarget, previewRef.current)}
            onDragOver={(e) => { if (e.dataTransfer.types.includes("Files")) { e.preventDefault(); setDragging(true); } }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              setDragging(false);
              const fs = imageFiles(e.dataTransfer.files);
              if (fs.length === 0) return;
              e.preventDefault();
              images.addFiles(fs);
            }}
            onPaste={(e) => {
              const fs = imageFiles(e.clipboardData.files);
              if (fs.length === 0) return; // 文字の貼り付けは、そのまま
              e.preventDefault();
              images.addFiles(fs);
            }}
          />
          {images.items.length > 0 && (
            <ul className="upload-list" data-testid="uploads" aria-label="画像のアップロード">
              {images.items.map((u) => (
                <li key={u.id} className={`upload-item ${u.status}`} data-status={u.status}>
                  <span className="upload-name">{u.name}</span>
                  {u.status === "uploading" && <progress max={1} value={u.progress} aria-label={`${u.name}のアップロード`} />}
                  {u.status === "done" && <span className="hint">本文に入れました（{sizeText(u.size)} → {u.optimized ? sizeText(u.optimized) : "最適化済み"}）</span>}
                  {u.status === "error" && (
                    <>
                      <span className="upload-error">{u.error}</span>
                      {u.error && !/^(JPEG|GIF|10MiB)/.test(u.error) && <Button variant="default" fullWidth={false} onClick={() => images.retry(u.id)}>再試行</Button>}
                      <Button variant="default" fullWidth={false} onClick={() => images.dismiss(u.id)}>閉じる</Button>
                    </>
                  )}
                </li>
              ))}
            </ul>
          )}
          <p className="hint">見出し・リスト・表・リンク・画像が使えます。画像はJPEG・PNG・WebP（10MiBまで）。アップロードすると縮小・WebP化して、撮影場所などの情報（EXIF）を消して保存します。YouTubeはURLを書くと動画が表示されます。</p>
        </section>

        <section className="editor-preview" aria-label="プレビュー">
          <div className="preview-head">
            <h2>プレビュー</h2>
            <SegmentedControl label="プレビューの幅" value={device} onChange={setDevice} options={[{ value: "pc", label: "PC" }, { value: "sp", label: "スマホ" }]} />
          </div>
          <div className="preview-scroll" ref={previewRef} onScroll={(e) => sync(e.currentTarget, mdRef.current)}>
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
          </div>
        </section>
      </div>

      {doc.revisions && doc.revisions.length > 1 && (
        <details className="panel editor-history">
          <summary>保存の履歴</summary>
          <ul className="items">
            {doc.revisions.map((r) => (
              <li key={r.revisionNo} className="item">
                <div>
                  {r.title}{" "}
                  {r.revisionNo === w.revisionNo && <Badge tone="neutral" icon={false}>最新</Badge>}{" "}
                  {r.revisionNo === published && <Badge tone="success">公開中</Badge>}
                  <div className="user-id">{r.editedBy}さん ・ {new Date(r.createdAt).toLocaleString("ja-JP")}</div>
                </div>
              </li>
            ))}
          </ul>
        </details>
      )}

      <Dialog
        open={confirm !== null}
        title={confirm === "publish" ? (published === null ? "公開しますか？" : "公開内容を更新しますか？") : confirm === "unpublish" ? "公開を止めますか？" : "削除しますか？"}
        onClose={() => setConfirm(null)}
        actions={confirm === "publish"
          ? <Button fullWidth={false} onClick={onPublish}>{published === null ? "公開する" : "更新する"}</Button>
          : confirm === "unpublish"
            ? <Button variant="danger" fullWidth={false} onClick={onUnpublish}>公開を止める</Button>
            : <Button variant="danger" fullWidth={false} onClick={onDelete}>削除する</Button>}
      >
        {confirm === "publish" && <p>{dirty ? "編集中の内容を保存して、" : ""}「{form.title || w.title}」を公式サイトに{published === null ? "公開します" : "反映します"}。</p>}
        {confirm === "unpublish" && <p>公式サイトから見えなくなります（下書きは残ります）。</p>}
        {confirm === "delete" && (published !== null
          ? <p>「{w.title}」を削除します。<strong>公開中の{doc.type === "project" ? "作品" : "記事"}なので、公式サイトからも見えなくなります。</strong>一覧からも消えます。</p>
          : <p>「{w.title}」を削除します。一覧から消えます。</p>)}
      </Dialog>
    </main>
  );
}
