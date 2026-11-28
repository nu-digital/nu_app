/**
 * モーションの共通設計（docs/design/ui-overhaul-2.md §3）。時間とイージングはここと app/ui.css の --motion-* を同じ値にそろえる。
 * 方針: 操作を待たせない（短く）・動かすのは transform と opacity だけ・prefers-reduced-motion では移動と拡大をやめる（MotionConfig と CSS の両方）。
 */
export const duration = { fast: 0.12, base: 0.2, slow: 0.36, hero: 0.8 } as const;
export const ease = { out: [0.22, 1, 0.36, 1], inOut: [0.65, 0, 0.35, 1] } as const;
/** 出入りの基本（フェード＋わずかな移動） */
export const fadeUp = { initial: { opacity: 0, y: 8 }, animate: { opacity: 1, y: 0 }, exit: { opacity: 0, y: 4 }, transition: { duration: duration.base, ease: ease.out } } as const;
/** メインビジュアルを自動で切り替える間隔（ミリ秒） */
export const HERO_INTERVAL_MS = 5500;
