"use client";

import { m } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";

import AppHeader from "@/components/AppHeader";
import HomeHero from "@/components/HomeHero";
import Button from "@/components/ui/Button";
import FeatureIcon from "@/components/ui/FeatureIcon";
import Link from "next/link";
import Badge from "@/components/ui/Badge";
import { describe, fetchMe, listContents, listItems, listPublicContents, logout, type ContentListItem, type Item, type Me } from "@/lib/api";
import { FEATURES, SHORTCUTS } from "@/lib/features";
import { heroSlides, imageOf, publicHref, publicSiteBase, recentOf, type PublicItem } from "@/lib/home";
import { duration, ease } from "@/lib/motion";

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
          <div className="my-box"><h3><FeatureIcon name="24/Archive" size={16} /> 借りている物品</h3>
            <ul>{items.slice(0, 5).map((it) => (<li key={it.id}><Link href={`/inventory/${it.id}`}>{it.name}</Link> <Badge tone="info">{it.borrowedByMe}個</Badge></li>))}</ul>
            <p className="hint">返却は、物品の詳細画面から登録できます。</p>
          </div>
        )}
        {drafts && drafts.length > 0 && (
          <div className="my-box"><h3><FeatureIcon name="24/Pencil" size={16} /> 書きかけの下書き</h3>
            <ul>{drafts.slice(0, 5).map((c) => (<li key={c.id}><Link href={`/contents/${c.id}`}>{c.title}</Link></li>))}</ul>
          </div>
        )}
      </div>
    </section>
  );
}

/** 最近公開された記事・作品（公開サイトと同じ、匿名でも読める公開版だけ） */
function Recent({ title, items, base, more }: { title: string; items: PublicItem[]; base: string | null; more: string }) {
  if (items.length === 0) return null;
  return (
    <section className="recent" aria-label={title}>
      <div className="recent-head"><h2>{title}</h2><Link href={more}>すべて見る →</Link></div>
      <ul className="recent-grid">
        {items.map((it, k) => {
          const img = imageOf(it), href = publicHref(it, base);
          const body = (
            <>
              <div className="recent-thumb">
                {img ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={img} alt="" loading="lazy" />
                ) : <span aria-hidden="true">{it.type === "project" ? "作品" : "記事"}</span>}
              </div>
              <div className="recent-body"><strong>{it.title}</strong><span className="hint">{it.date?.label}</span></div>
            </>
          );
          return (
            <m.li key={`${it.section}/${it.slug}`} initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: duration.base, ease: ease.out, delay: k * 0.05 }}>
              {href ? <a className="recent-card" href={href} target="_blank" rel="noopener noreferrer">{body}</a> : <Link className="recent-card" href={more}>{body}</Link>}
            </m.li>
          );
        })}
      </ul>
    </section>
  );
}

export default function Launcher() {
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [pub, setPub] = useState<PublicItem[] | null>(null);
  const base = useMemo(() => (typeof window === "undefined" ? null : publicSiteBase(window.location.hostname)), []);

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

  // メインビジュアル・最近の公開は、ログイン後に取得する（取得できなくてもホームは使える）
  useEffect(() => {
    if (!me) return;
    let alive = true;
    listPublicContents(24).then((r) => alive && setPub(r.items)).catch(() => alive && setPub([]));
    return () => { alive = false; };
  }, [me]);

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

  const slides = heroSlides(pub ?? [], base);
  // 公開できる作品・記事が無い（または取得できない）ときは、団体の写真を出す
  const fallback = [{ key: "nu_digital", image: "/images/hero.webp", title: "nu_digital", kind: "記事" as const, dateLabel: "", href: null }];

  return (
    <>
      <AppHeader displayName={me.user.displayName} onLogout={onLogout} busy={busy} showNav={false} />
      <HomeHero slides={pub === null || slides.length === 0 ? fallback : slides} greeting={`${me.user.displayName}さん、こんにちは`} sub="日本大学デジタル創作研究会 nu_digital" />
      <main className="home-main">
        <MySummary />
        <section aria-label="ショートカット">
          <ul className="shortcuts">
            {SHORTCUTS.map((s) => (
              <li key={s.key}><Link className="shortcut" href={s.href}><FeatureIcon name={s.icon} size={16} />{s.label}</Link></li>
            ))}
          </ul>
        </section>
        <section aria-labelledby="apps-title">
          <h2 id="apps-title" className="visually-hidden">機能</h2>
          <ul className="tiles">
            {FEATURES.map((f, k) => (
              <m.li key={f.key} initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: duration.base, ease: ease.out, delay: k * 0.04 }}>
                <Link className={`tile tone-${f.tone}`} href={f.href}>
                  <span className="tile-icon"><FeatureIcon name={f.icon} size={32} /></span>
                  <span className="tile-text"><strong>{f.title}</strong><span>{f.description}</span></span>
                </Link>
              </m.li>
            ))}
          </ul>
        </section>
        <Recent title="最近公開された作品" items={recentOf(pub ?? [], "project")} base={base} more="/contents?type=project" />
        <Recent title="最近公開された記事" items={recentOf(pub ?? [], "article")} base={base} more="/contents?type=article" />
      </main>
    </>
  );
}
