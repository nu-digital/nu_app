"use client";

import { useEffect, useRef, type ReactNode } from "react";

import Button from "@/components/ui/Button";

/**
 * 団体の共通ダイアログ（ネイティブの <dialog> + showModal）。
 * Escで閉じる・背景は操作不可・閉じたら開いたボタンにフォーカスが戻る・フォーカスは中に閉じ込められる、をブラウザが担う。
 * 本文は children。フッターのボタンは actions。変更を保存する操作には「キャンセル」を必ず置く。
 */
export default function Dialog({ open, title, onClose, children, actions }: { open: boolean; title: string; onClose: () => void; children: ReactNode; actions?: ReactNode }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open && !el.open) el.showModal();
    if (!open && el.open) el.close();
  }, [open]);
  return (
    <dialog ref={ref} className="ui-dialog" aria-labelledby="ui-dialog-title" onClose={onClose} onClick={(e) => { if (e.target === ref.current) onClose(); }}>
      {open && (
        <div className="ui-dialog-body">
          <h2 id="ui-dialog-title">{title}</h2>
          {children}
          <div className="actions">
            {actions}
            <Button variant="default" fullWidth={false} onClick={onClose}>キャンセル</Button>
          </div>
        </div>
      )}
    </dialog>
  );
}
