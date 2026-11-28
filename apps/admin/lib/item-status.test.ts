import assert from "node:assert/strict";
import test from "node:test";

import { itemStatus, stockText } from "./item-status.ts";

const q = (total: number, available: number, mine = 0) => ({ mode: "quantity" as const, totalQuantity: total, unitCount: 0, available, borrowedByMe: mine });
const ind = (units: number, available: number, mine = 0) => ({ mode: "individual" as const, totalQuantity: 0, unitCount: units, available, borrowedByMe: mine });

test("貸出可能・一部貸出中・すべて貸出中を区別する（数量管理）", () => {
  assert.equal(itemStatus(q(5, 5)).key, "available");
  assert.equal(itemStatus(q(5, 2)).key, "partial");
  assert.equal(itemStatus(q(5, 0)).key, "none");
  assert.equal(itemStatus(q(5, 2)).note, "5個のうち2個が借りられます");
  assert.equal(stockText(itemStatus(q(5, 2))), "残り 2 / 5");
});

test("個体管理: 1個体だけなら『一部』にならない。個体が無ければ未登録", () => {
  assert.equal(itemStatus(ind(1, 1)).key, "available");
  assert.equal(itemStatus(ind(1, 0)).key, "none");
  assert.equal(itemStatus(ind(2, 1)).key, "partial");
  assert.equal(itemStatus(ind(0, 0)).key, "empty");
  assert.equal(itemStatus(ind(0, 0)).label, "個体が未登録");
  assert.equal(stockText(itemStatus(ind(1, 1))), "");
});

test("自分が借りている数は状態と独立に持つ。在庫が負でも0に丸める", () => {
  const s = itemStatus(q(3, 0, 2));
  assert.equal(s.key, "none");
  assert.equal(s.mine, 2);
  assert.equal(itemStatus(q(3, -1)).available, 0);
});
