// 開発用モックの応答の検査（node --test。Node 22.18以上の型除去で実行する）。
import assert from "node:assert/strict";
import test from "node:test";

import { createMock } from "./handler.ts";

test("未ログインでは401、ロール名のログインIDでログインできる", () => {
  const r = createMock();
  assert.equal(r("GET", "/api/v1/auth/me", null).status, 401);
  const login = r("POST", "/api/v1/auth/login", null, { loginId: "editor", password: "x" });
  assert.equal(login.status, 200);
  assert.equal(login.setRole, "editor");
  assert.equal(r("POST", "/api/v1/auth/login", null, { loginId: "nobody", password: "x" }).status, 401);
});

test("管理APIはadmin・ownerだけ、owner専用APIはownerだけ", () => {
  const r = createMock();
  assert.equal(r("GET", "/api/v1/admin/users", "member").status, 403);
  assert.equal(r("GET", "/api/v1/admin/users", "editor").status, 403);
  assert.equal(r("GET", "/api/v1/admin/users", "admin").status, 200);
  assert.equal(r("PUT", "/api/v1/owner/users/u-member/admin-role", "admin", { enabled: true }).status, 403);
  assert.equal(r("PUT", "/api/v1/owner/users/u-member/admin-role", "owner", { enabled: true }).status, 200);
});

test("貸出すると残数が減り、返却すると戻る", () => {
  const r = createMock();
  const before = (r("GET", "/api/v1/inventory/items/i-tripod", "member").json as { available: number }).available;
  const loan = r("POST", "/api/v1/inventory/loans", "member", { itemId: "i-tripod", quantity: 2 });
  assert.equal(loan.status, 201);
  assert.equal((r("GET", "/api/v1/inventory/items/i-tripod", "member").json as { available: number }).available, before - 2);
  r("POST", `/api/v1/inventory/loans/${(loan.json as { id: string }).id}/returns`, "member", { quantity: 2 });
  assert.equal((r("GET", "/api/v1/inventory/items/i-tripod", "member").json as { available: number }).available, before);
  assert.equal(r("POST", "/api/v1/inventory/loans", "member", { itemId: "i-tablet", quantity: 1 }).status, 409);
});

test("空・失敗の状態を再現できる", () => {
  assert.deepEqual((createMock({ empty: true })("GET", "/api/v1/inventory/items", "member").json as { items: unknown[] }).items, []);
  assert.equal(createMock({ failPrefix: "/inventory" })("GET", "/api/v1/inventory/items", "member").status, 500);
});

test("未定義のAPIは404で、名前を返す", () => {
  const res = createMock()("GET", "/api/v1/nope", "member");
  assert.equal(res.status, 404);
  assert.match(JSON.stringify(res.json), /nope/);
});
