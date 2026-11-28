"use client";

import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { useCallback, useEffect, useState } from "react";

import IconButton from "@/components/ui/IconButton";
import { duration, ease, HERO_INTERVAL_MS } from "@/lib/motion";
import type { Slide } from "@/lib/home";

/**
 * ホームのメインビジュアル。公開中の作品・記事のカバーを、ゆっくりクロスフェードで切り替える。
 * - 自動で切り替える（約5.5秒）。ホバー・キーボードのフォーカス・「一時停止」で止まり、OSの「視差効果を減らす」では最初から自動で動かさない。
 * - 手動で前後・番号で切り替えられる。切り替えの前に、次の画像を先読みする。
 * - 関連する記事・作品（公開サイト）へ移動できる。
 */
export default function HomeHero({ slides, greeting, sub }: { slides: Slide[]; greeting: string; sub: string }) {
  const reduce = useReducedMotion();
  const [i, setI] = useState(0);
  const [held, setHeld] = useState(false); // ホバー・フォーカス中
  const [paused, setPaused] = useState(false); // 利用者が止めた
  const n = slides.length;
  const cur = slides[Math.min(i, Math.max(0, n - 1))];

  const go = useCallback((to: number) => setI(((to % n) + n) % n), [n]);

  useEffect(() => {
    if (n < 2 || held || paused || reduce) return;
    const t = setInterval(() => { if (!document.hidden) setI((x) => (x + 1) % n); }, HERO_INTERVAL_MS);
    return () => clearInterval(t);
  }, [n, held, paused, reduce]);

  // 次の画像を先読みして、切り替えの瞬間に待たせない
  useEffect(() => {
    if (n < 2) return;
    const next = slides[(i + 1) % n];
    if (next) { const img = new window.Image(); img.src = next.image; }
  }, [i, n, slides]);

  if (!cur) return null;
  const caption = (
    <>
      <span className={`hero-kind kind-${cur.kind === "作品" ? "project" : "article"}`}>{cur.kind}</span>
      <span className="hero-title">{cur.title}</span>
      {cur.dateLabel && <span className="hero-date">{cur.dateLabel}</span>}
    </>
  );

  return (
    <section
      className="home-hero"
      aria-roledescription="カルーセル"
      aria-label="活動と作品のハイライト"
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)}
      onBlur={() => setHeld(false)}
    >
      <div className="hero-stage" aria-live={paused || held ? "polite" : "off"}>
        <AnimatePresence initial={false}>
          <m.div
            key={cur.key}
            className="hero-slide"
            initial={{ opacity: 0, scale: 1.04 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: duration.hero, ease: ease.inOut }}
            data-testid="hero-slide"
            data-key={cur.key}
          >
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src={cur.image} alt="" />
          </m.div>
        </AnimatePresence>
        <div className="hero-shade" aria-hidden="true" />
      </div>

      <div className="hero-overlay">
        <div className="hero-greeting">
          <h1 id="hello">{greeting}</h1>
          <p>{sub}</p>
        </div>
        <div className="hero-bottom">
          {cur.href ? (
            <a className="hero-caption" href={cur.href} target="_blank" rel="noopener noreferrer" aria-label={`${cur.title}（${cur.kind}）を公式サイトで見る`}>
              {caption}<span className="hero-more">公式サイトで見る →</span>
            </a>
          ) : <div className="hero-caption">{caption}</div>}
          {n > 1 && (
            <div className="hero-controls">
              <IconButton icon="24/Prev" label="前のスライド" onClick={() => go(i - 1)} />
              <ul className="hero-dots" aria-label="スライドの選択">
                {slides.map((s, k) => (
                  <li key={s.key}><button type="button" aria-label={`${k + 1}枚目: ${s.title}`} aria-current={k === i} onClick={() => go(k)} /></li>
                ))}
              </ul>
              <IconButton icon="24/Next" label="次のスライド" onClick={() => go(i + 1)} />
              {!reduce && <IconButton icon={paused ? "24/Play" : "24/Pause"} label={paused ? "自動で切り替える" : "自動の切り替えを止める"} aria-pressed={paused} onClick={() => setPaused((p) => !p)} />}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
