import type { Item } from "./api";

/**
 * 物品の貸出状態を、利用者に伝わる形にまとめる（一覧・詳細で共通）。
 * - 貸出可能: 全部借りられる／一部貸出中: 残りはあるが貸出中もある／すべて貸出中: 借りられない／未登録: 個体管理で個体が無い
 * - 自分が借りているかは別のフラグ（mine）。状態と同時に出せる。
 * 色だけに頼らず、文字と記号で区別する（表示は components/ui/Badge）。
 */
export type ItemStatusKey = "available" | "partial" | "none" | "empty";
export type ItemStatus = { key: ItemStatusKey; label: string; tone: "success" | "warning" | "danger" | "neutral"; total: number; available: number; mine: number; note: string };

export function itemStatus(it: Pick<Item, "mode" | "totalQuantity" | "unitCount" | "available" | "borrowedByMe">): ItemStatus {
  const total = it.mode === "quantity" ? it.totalQuantity : it.unitCount;
  const available = Math.max(0, it.available);
  const mine = it.borrowedByMe ?? 0;
  const base = { total, available, mine };
  if (total === 0) return { ...base, key: "empty", label: it.mode === "individual" ? "個体が未登録" : "在庫なし", tone: "neutral", note: it.mode === "individual" ? "管理者が個体を登録するまで借りられません" : "" };
  if (available === 0) return { ...base, key: "none", label: "すべて貸出中", tone: "danger", note: "返却されるまで借りられません" };
  if (available >= total) return { ...base, key: "available", label: "貸出可能", tone: "success", note: total > 1 ? `${total}個すべて借りられます` : "" };
  return { ...base, key: "partial", label: "一部貸出中", tone: "warning", note: `${total}個のうち${available}個が借りられます` };
}

/** 「残り 3 / 5」のような短い在庫表示（総数が1個以下のときは空） */
export function stockText(s: ItemStatus): string {
  return s.total > 1 ? `残り ${s.available} / ${s.total}` : "";
}
