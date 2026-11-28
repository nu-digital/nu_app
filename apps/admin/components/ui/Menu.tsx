"use client";

import { useEffect, useId, useRef, useState, type ReactNode } from "react";

import IconButton from "@/components/ui/IconButton";

export type MenuItem = { key: string; label: string; icon?: ReactNode; danger?: boolean; disabled?: boolean; onSelect: () => void };

/**
 * 三点メニュー（⋯）。クリックで開き、項目を選ぶ・外をクリック・Escで閉じる。↑↓で項目を移動できる。
 * ボタンの読み上げ名は label（例: 「「〇〇」のメニュー」）。項目が空なら何も出さない。
 */
export default function Menu({ label, items, align = "right" }: { label: string; items: MenuItem[]; align?: "left" | "right" }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const id = useId();

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => { if (root.current && !root.current.contains(e.target as Node)) setOpen(false); };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") { setOpen(false); root.current?.querySelector<HTMLButtonElement>("button[aria-haspopup]")?.focus(); }
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        const els = Array.from(root.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? []);
        if (!els.length) return;
        e.preventDefault();
        const i = els.indexOf(document.activeElement as HTMLButtonElement);
        els[(i + (e.key === "ArrowDown" ? 1 : -1) + els.length) % els.length].focus();
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    root.current?.querySelector<HTMLButtonElement>('[role="menuitem"]:not(:disabled)')?.focus();
    return () => { document.removeEventListener("mousedown", onDown); document.removeEventListener("keydown", onKey); };
  }, [open]);

  if (items.length === 0) return null;
  return (
    <div className="ui-menu" ref={root}>
      <IconButton icon="24/Dot" label={label} aria-haspopup="menu" aria-expanded={open} aria-controls={id} onClick={() => setOpen((o) => !o)} />
      {open && (
        <ul id={id} className={`ui-menu-list align-${align}`} role="menu">
          {items.map((it) => (
            <li key={it.key} role="none">
              <button type="button" role="menuitem" className={it.danger ? "danger" : undefined} disabled={it.disabled} onClick={() => { setOpen(false); it.onSelect(); }}>
                {it.icon}{it.label}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
