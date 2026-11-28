"use client";

import { LazyMotion, MotionConfig } from "motion/react";
import type { ReactNode } from "react";

// 機能は初期表示のあとで読み込む（ページの表示・データ取得を遅くしない）。読み込めるまでは動かさず、そのまま表示される。
const loadFeatures = () => import("@/lib/motion-features").then((m) => m.default);

/** アプリ全体のモーション設定。OSの「視差効果を減らす」設定では、移動・拡大をやめて透明度の変化だけにする。 */
export default function MotionProvider({ children }: { children: ReactNode }) {
  return (
    <MotionConfig reducedMotion="user">
      <LazyMotion features={loadFeatures} strict>{children}</LazyMotion>
    </MotionConfig>
  );
}
