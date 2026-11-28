import Badge from "@/components/ui/Badge";
import { itemStatus, stockText } from "@/lib/item-status";
import type { Item } from "@/lib/api";

/** 物品の状態の札（貸出可能／一部貸出中／すべて貸出中＋自分が借りている）。文字と記号で状態が分かる。 */
export default function ItemStatusBadges({ item, showStock = true }: { item: Pick<Item, "mode" | "totalQuantity" | "unitCount" | "available" | "borrowedByMe">; showStock?: boolean }) {
  const s = itemStatus(item);
  const stock = showStock ? stockText(s) : "";
  return (
    <span className="item-status">
      <Badge tone={s.tone}>{s.label}</Badge>
      {stock && <span className="stock-text">{stock}</span>}
      {s.mine > 0 && <Badge tone="info">自分が借りている{s.mine > 1 ? `（${s.mine}個）` : ""}</Badge>}
    </span>
  );
}
