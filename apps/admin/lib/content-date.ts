import type { ContentDate } from "./api";

/**
 * 記事・作品の日付は、利用者には「日付」1つだけを見せる（docs/design/ui-overhaul.md §5.2）。
 * 内部の表示ラベル・種類・並び順の日付は、ここで自動的に決める。
 * - 日付を選び直したとき: 「その日」（kind=day・ラベル=YYYY年M月D日）にする
 * - 触っていないとき: 既存の値（「2025年12月」「期間」「不明」など、公開サイトの表示）をそのまま保つ
 */
export type DatePayload = { label: string; kind: "day" | "month" | "range" | "unknown"; sortDate: string | null };

export function labelFromDate(iso: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!m) return "";
  return `${Number(m[1])}年${Number(m[2])}月${Number(m[3])}日`;
}

export function todayIso(now = new Date()): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}`;
}

/** 編集欄に出す日付（YYYY-MM-DD。未設定なら空） */
export const inputFromDate = (d: Pick<ContentDate, "sortDate">): string => d.sortDate ?? "";

/** 保存する日付。入力が元の値と同じなら元をそのまま、違えば入力から決める。 */
export function resolveDate(original: ContentDate, input: string): DatePayload {
  if (input === inputFromDate(original)) return { label: original.label, kind: original.kind, sortDate: original.sortDate };
  if (!input) return { label: "", kind: "unknown", sortDate: null };
  return { label: labelFromDate(input), kind: "day", sortDate: input };
}

/** 元の日付が「その日」以外のとき、公開サイトに出ている表示を伝える（選び直すと変わる旨の案内用） */
export const originalLabelNote = (d: ContentDate, input: string): string | null =>
  d.kind !== "day" && d.label && input === inputFromDate(d) ? d.label : null;
