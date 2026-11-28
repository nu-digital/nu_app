import assert from "node:assert/strict";
import test from "node:test";

import { inputFromDate, labelFromDate, originalLabelNote, resolveDate, todayIso } from "./content-date.ts";

const day = { label: "2025年12月3日", kind: "day" as const, sortDate: "2025-12-03", sortDateIsEstimated: false };
const month = { label: "2025年12月", kind: "month" as const, sortDate: "2025-12-01", sortDateIsEstimated: true };
const range = { label: "2026年8月1〜2日", kind: "range" as const, sortDate: "2026-08-01", sortDateIsEstimated: true };
const unknown = { label: "", kind: "unknown" as const, sortDate: null, sortDateIsEstimated: false };

test("日付を選び直したら『その日』になる。0埋めしない表示", () => {
  assert.equal(labelFromDate("2025-01-05"), "2025年1月5日");
  assert.deepEqual(resolveDate(unknown, "2026-11-28"), { label: "2026年11月28日", kind: "day", sortDate: "2026-11-28" });
  assert.deepEqual(resolveDate(day, "2025-12-04"), { label: "2025年12月4日", kind: "day", sortDate: "2025-12-04" });
});

test("触っていなければ既存の表示・種類・並び順の日付をそのまま保つ（公開サイトの表示を壊さない）", () => {
  assert.deepEqual(resolveDate(month, inputFromDate(month)), { label: "2025年12月", kind: "month", sortDate: "2025-12-01" });
  assert.deepEqual(resolveDate(range, inputFromDate(range)), { label: "2026年8月1〜2日", kind: "range", sortDate: "2026-08-01" });
  assert.deepEqual(resolveDate(unknown, ""), { label: "", kind: "unknown", sortDate: null });
});

test("日付を消したら未設定（不明）になる", () => {
  assert.deepEqual(resolveDate(day, ""), { label: "", kind: "unknown", sortDate: null });
});

test("既存が『その日』以外で未変更のときだけ、公開サイトの表示を案内する", () => {
  assert.equal(originalLabelNote(month, "2025-12-01"), "2025年12月");
  assert.equal(originalLabelNote(month, "2025-12-02"), null);
  assert.equal(originalLabelNote(day, "2025-12-03"), null);
});

test("今日の日付はYYYY-MM-DD", () => {
  assert.equal(todayIso(new Date(2026, 10, 8)), "2026-11-08");
});
