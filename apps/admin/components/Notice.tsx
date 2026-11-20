"use client";

import { useEffect, useRef } from "react";

/** 完了・エラーの表示。表示されたらフォーカスを移して伝える（色だけに頼らず「エラー」「完了」の文字つき）。 */
export default function Notice({ kind, children }: { kind: "error" | "success"; children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.focus();
  }, [children]);
  return (
    <div className={`notice ${kind}`} role={kind === "error" ? "alert" : "status"} tabIndex={-1} ref={ref}>
      <strong>{kind === "error" ? "エラー" : "完了"}</strong>
      <span>{children}</span>
    </div>
  );
}
