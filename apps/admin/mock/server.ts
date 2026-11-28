// 開発用モックAPIのHTTPサーバー。Docker・Go・DBなしで、ログイン後の画面を架空データで確認するためのもの。
//   node apps/admin/mock/server.ts [--port 18390]
// 通常は `npm run admin:dev:mock`（scripts/admin-dev-mock.mjs）がこのサーバーとadminのnext devを一緒に起動する。
// 環境変数: MOCK_DELAY_MS（応答を遅らせる）、MOCK_EMPTY=1（一覧を空にする）、MOCK_FAIL=/inventory（そのパスで始まるAPIを500にする）。
// 本番では使わない: NODE_ENV=production では起動を拒否し、127.0.0.1 でだけ待ち受ける。
import { createServer } from "node:http";

import type { Role } from "./fixtures.ts";
import { createMock } from "./handler.ts";

if (process.env.NODE_ENV === "production") {
  console.error("開発用モックは NODE_ENV=production では起動しません。");
  process.exit(1);
}

const arg = (name: string, d: string) => {
  const i = process.argv.indexOf(`--${name}`);
  return i < 0 ? d : process.argv[i + 1];
};
const port = Number(arg("port", process.env.MOCK_ADMIN_API_PORT ?? "18390"));
const delay = Number(process.env.MOCK_DELAY_MS ?? 0);
const respond = createMock({ empty: process.env.MOCK_EMPTY === "1", failPrefix: process.env.MOCK_FAIL || undefined });
const COOKIE = "nu_mock_role";
const ROLES = new Set(["owner", "admin", "editor", "member"]);

createServer((req, res) => {
  const chunks: Buffer[] = [];
  req.on("data", (c: Buffer) => chunks.push(c));
  req.on("end", () => {
    const cookie = /(?:^|;\s*)nu_mock_role=([a-z]+)/.exec(req.headers.cookie ?? "")?.[1];
    const role = cookie && ROLES.has(cookie) ? (cookie as Role) : null;
    let body: unknown;
    const text = Buffer.concat(chunks).toString("utf8");
    if (text && (req.headers["content-type"] ?? "").includes("json")) {
      try {
        body = JSON.parse(text);
      } catch {
        body = undefined;
      }
    }
    const r = respond(req.method ?? "GET", req.url ?? "/", role, body);
    const headers: Record<string, string> = { "Cache-Control": "no-store" };
    if (r.setRole !== undefined) headers["Set-Cookie"] = r.setRole ? `${COOKIE}=${r.setRole}; Path=/; HttpOnly; SameSite=Lax` : `${COOKIE}=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax`;
    setTimeout(() => {
      if (r.raw !== undefined) {
        res.writeHead(r.status, { ...headers, "Content-Type": r.contentType ?? "text/plain" });
        res.end(r.raw);
        return;
      }
      if (r.json !== undefined) headers["Content-Type"] = "application/json";
      res.writeHead(r.status, headers);
      res.end(r.json === undefined ? "" : JSON.stringify(r.json));
    }, delay);
  });
}).listen(port, "127.0.0.1", () => {
  console.log(`開発用モックAPI: http://127.0.0.1:${port}/api/v1 （データはすべて架空）`);
  console.log("ログインID: owner / admin / editor / member（パスワードは何でもよい）");
});
