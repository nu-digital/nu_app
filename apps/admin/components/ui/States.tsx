import type { ReactNode } from "react";

/** 読み込み中（スピナー+文字）。aria-busy で伝える。 */
export function Loading({ text = "読み込んでいます…" }: { text?: string }) {
  return (
    <p className="ui-loading" aria-busy="true" role="status">
      <span className="ui-spinner" aria-hidden="true" />
      {text}
    </p>
  );
}

/** 一覧の読み込み中の骨組み（カード形）。 */
export function CardSkeleton({ count = 8 }: { count?: number }) {
  return (
    <ul className="grid" aria-busy="true" aria-label="読み込み中">
      {Array.from({ length: count }, (_, i) => (
        <li key={i} className="ui-skeleton-card" aria-hidden="true"><div className="ui-skeleton thumb-sk" /><div className="ui-skeleton line" /><div className="ui-skeleton line short" /></li>
      ))}
    </ul>
  );
}

/** データが無いときの表示。次の行動（action）を示す。 */
export function EmptyState({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="ui-empty">
      <p className="ui-empty-title">{title}</p>
      {children && <p className="hint">{children}</p>}
      {action}
    </div>
  );
}
