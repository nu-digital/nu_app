"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";

import AppHeader from "@/components/AppHeader";
import Button from "@/components/ui/Button";
import Link from "next/link";
import Badge from "@/components/ui/Badge";
import { describe, fetchMe, listContents, listItems, logout, type ContentListItem, type Item, type Me } from "@/lib/api";

// アプリの定義（コード上の一覧）。全員に同じカードを表示する。権限はAPIが強制するため、画面の表示・非表示では制御しない。
// 実装前のものは「準備中」と明示して押せないようにする。
type AppDef = { id: string; title: string; description: string; href?: string };
const APPS: AppDef[] = [
  { id: "inventory", title: "物品", description: "団体の機材を探して、貸出・返却を記録します。", href: "/inventory" },
  { id: "articles", title: "記事・作品", description: "活動報告や作品紹介を書いて、公開します。", href: "/contents" },
  { id: "members", title: "メンバー", description: "メンバーを探して、プロフィールを見ます。", href: "/members" },
  { id: "profile", title: "プロフィール", description: "自分の表示名や公開する内容を設定します。", href: "/profile" },
  { id: "users", title: "ユーザー管理", description: "ユーザーの追加・停止や、招待・再設定URLの発行をします（管理者向け）。", href: "/admin/users" },
];

/** 自分の状況: 借りている物品と、書きかけの下書き（取りこぼしを防ぐ）。取得できなければ何も出さない。 */
function MySummary() {
  const [items, setItems] = useState<Item[] | null>(null);
  const [drafts, setDrafts] = useState<ContentListItem[] | null>(null);
  useEffect(() => {
    let alive = true;
    listItems({ mine: true }).then((r) => alive && setItems(r.items)).catch(() => alive && setItems([]));
    listContents({ mine: true, status: "draft" }).then((r) => alive && setDrafts(r.items.filter((c) => c.canEdit))).catch(() => alive && setDrafts([]));
    return () => { alive = false; };
  }, []);
  if ((items?.length ?? 0) === 0 && (drafts?.length ?? 0) === 0) return null;
  return (
    <section className="my-summary" aria-labelledby="my-title">
      <h2 id="my-title">あなたの状況</h2>
      <div className="my-grid">
        {items && items.length > 0 && (
          <div className="my-box"><h3>借りている物品</h3>
            <ul>{items.slice(0, 5).map((it) => (<li key={it.id}><Link href={`/inventory/${it.id}`}>{it.name}</Link> <Badge tone="info">{it.borrowedByMe}個</Badge></li>))}</ul>
            <p className="hint">返却は、物品の詳細か「貸出中の一覧」から登録できます。</p>
          </div>
        )}
        {drafts && drafts.length > 0 && (
          <div className="my-box"><h3>書きかけの下書き</h3>
            <ul>{drafts.slice(0, 5).map((c) => (<li key={c.id}><Link href={`/contents/${c.id}`}>{c.title}</Link></li>))}</ul>
          </div>
        )}
      </div>
    </section>
  );
}

export default function Launcher() {
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    fetchMe()
      .then((m) => {
        if (!alive) return;
        if (!m) router.replace("/login");
        else setMe(m);
      })
      .catch((e) => alive && setError(describe(e)));
    return () => {
      alive = false;
    };
  }, [router]);

  async function onLogout() {
    if (busy) return;
    setBusy(true);
    try {
      await logout();
      router.replace("/login");
    } catch (e) {
      setError(describe(e));
      setBusy(false);
    }
  }

  if (error) {
    return (
      <main className="auth-wrap">
        <div className="auth-card">
          <div className="notice error" role="alert">
            <strong>エラー</strong>
            <span>{error}</span>
          </div>
          <Button type="submit" onClick={() => window.location.reload()}>
            もう一度読み込む
          </Button>
        </div>
      </main>
    );
  }
  if (!me) return <main aria-busy="true" />;

  return (
    <>
      <AppHeader displayName={me.user.displayName} onLogout={onLogout} busy={busy} showNav={false} />
      <section className="hero" aria-labelledby="hello">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/images/hero.webp" alt="nu_digitalのメンバーが集まっている写真" />
        <div className="hero-text">
          <h1 id="hello">{me.user.displayName}さん、こんにちは</h1>
          <p>日本大学デジタル創作研究会 nu_digital</p>
        </div>
      </section>
      <main className="launcher">
        <h2>アプリ</h2>
        <ul className="cards">
          {APPS.map((a) => (
            <li key={a.id}>
              {a.href ? (
                <a className="card" href={a.href}>
                  <h3>{a.title}</h3>
                  <p>{a.description}</p>
                </a>
              ) : (
                <div className="card soon" aria-disabled="true">
                  <h3>{a.title}</h3>
                  <p>{a.description}</p>
                  <span className="badge">準備中</span>
                </div>
              )}
            </li>
          ))}
        </ul>
        <MySummary />
      </main>
    </>
  );
}
