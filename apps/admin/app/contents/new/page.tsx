"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import TextField from "@/components/ui/TextField";
import Select from "@/components/ui/Select";
import { createContent, describe } from "@/lib/api";
import { labelFromDate, todayIso } from "@/lib/content-date";

export default function NewContentPage() {
  return <AppShell>{() => <NewContent />}</AppShell>;
}

const KINDS = {
  blog: { label: "記事（ブログ）", type: "article", section: "blog" },
  news: { label: "記事（お知らせ）", type: "article", section: "news" },
  project: { label: "作品", type: "project", section: "project" },
} as const;

function NewContent() {
  const router = useRouter();
  const [kind, setKind] = useState<keyof typeof KINDS>("blog");
  const [slug, setSlug] = useState("");
  const [title, setTitle] = useState("");
  const [date, setDate] = useState(() => todayIso());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  return (
    <main className="page">
      <p><Link href="/contents">← 記事・作品へ</Link></p>
      <h1>新しく書く</h1>
      <p className="lead">まず種類とタイトルを決めて下書きを作ります。本文はこのあと書きます。</p>
      {error && <Notice kind="error">{error}</Notice>}
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          if (busy) return;
          setBusy(true);
          setError("");
          try {
            const k = KINDS[kind];
            const c = await createContent({ type: k.type, section: k.section, slug: slug.trim(), title: title.trim(), markdown: "", date: date ? { label: labelFromDate(date), kind: "day", sortDate: date } : undefined });
            router.push(`/contents/${c.id}`);
          } catch (err) {
            setError(describe(err));
            setBusy(false);
          }
        }}
      >
        <Select id="n-kind" label="種類" value={kind} onChange={(v) => setKind(v as keyof typeof KINDS)}>
          {Object.entries(KINDS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
        </Select>
        <div className="field">
          <TextField id="n-title" label="タイトル（必須）" required maxLength={200} value={title} onChange={(v: string) => setTitle(v)} />
        </div>
        <div className="field">
          <TextField id="n-slug" label="URL用の名前（必須）" required maxLength={100} pattern="[A-Za-z0-9][A-Za-z0-9._\-]*" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={slug} onChange={(v: string) => setSlug(v)} />
          <p className="hint">英数字と . _ - だけ。公開サイトのURL（/{KINDS[kind].section}/<strong>{slug || "名前"}</strong>/）になり、<strong>作成後は変えられません</strong>。</p>
        </div>
        <div className="field">
          <label htmlFor="n-date">日付</label>
          <input id="n-date" className="input" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
          <p className="hint">公式サイトの表示と並び順に使われます。あとから変えられます。</p>
        </div>
        <Button fullWidth={false} type="submit" disabled={busy || !title.trim() || !slug.trim()}>下書きを作る</Button>
      </form>
    </main>
  );
}
