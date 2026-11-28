"use client";

import { AnimatePresence, m } from "motion/react";
import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";

import { duration, ease } from "@/lib/motion";

/**
 * 操作完了・失敗のトースト通知。画面の下に数秒表示して消える（aria-live で読み上げる）。
 * 重要なエラー（入力の修正が必要なもの）は、その場に残る components/Notice を使う。トーストは「保存しました」のような完了の確認用。
 */
type T = { id: number; kind: "success" | "error"; text: string };
const Ctx = createContext<(kind: T["kind"], text: string) => void>(() => {});

export const useToast = () => useContext(Ctx);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<T[]>([]);
  const seq = useRef(0);
  const push = useCallback((kind: T["kind"], text: string) => {
    const id = ++seq.current;
    setItems((cur) => [...cur.slice(-2), { id, kind, text }]);
    setTimeout(() => setItems((cur) => cur.filter((t) => t.id !== id)), kind === "error" ? 8000 : 4000);
  }, []);
  return (
    <Ctx.Provider value={push}>
      {children}
      <div className="ui-toasts" aria-live="polite">
        <AnimatePresence initial={false}>
          {items.map((t) => (
            <m.div
              key={t.id}
              layout
              className={`ui-toast ${t.kind}`}
              role={t.kind === "error" ? "alert" : "status"}
              initial={{ opacity: 0, y: 16 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 8 }}
              transition={{ duration: duration.base, ease: ease.out }}
            >
              <strong>{t.kind === "error" ? "エラー" : "完了"}</strong> {t.text}
            </m.div>
          ))}
        </AnimatePresence>
      </div>
    </Ctx.Provider>
  );
}
