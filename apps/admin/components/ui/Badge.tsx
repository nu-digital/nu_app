import type { ReactNode } from "react";

/**
 * 状態の札。**色だけに頼らず、文字と記号（icon）でも状態が分かる**ようにする。
 * tone: success（OK・使える）／warning（一部・注意）／danger（使えない・エラー）／info（自分に関係する）／neutral（その他）
 */
export type Tone = "success" | "warning" | "danger" | "info" | "neutral";
const ICON: Record<Tone, string> = { success: "✓", warning: "◐", danger: "✕", info: "★", neutral: "・" };

export default function Badge({ tone = "neutral", icon, children }: { tone?: Tone; icon?: string | false; children: ReactNode }) {
  const mark = icon === false ? null : (icon ?? ICON[tone]);
  return (
    <span className={`ui-badge tone-${tone}`}>
      {mark && <span aria-hidden="true">{mark}</span>}
      {children}
    </span>
  );
}
