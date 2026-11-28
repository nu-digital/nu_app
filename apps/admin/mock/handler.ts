// 開発用モックAPIの応答（HTTPに依存しない部分）。server.ts と、実APIとの契約比較（scripts/compose-smoke.mjs）が使う。
// 権限の出し分けは実API（docs/AUTHZ.md）に合わせた簡易版。本物の認可の検査には使わない。
import { ROLE_USERS, seed, type Role } from "./fixtures.ts";

export type MockOptions = { empty?: boolean; failPrefix?: string };
export type MockResponse = { status: number; json?: unknown; raw?: string; contentType?: string; setRole?: Role | null };

const err = (status: number, code: string, message: string): MockResponse => ({ status, json: { error: { code, message } } });
const page = <T>(items: T[]) => ({ items, nextCursor: null });

export function createMock(opts: MockOptions = {}) {
  const db = seed();
  let uploadSeq = 0;
  const isAdmin = (r: Role) => r === "admin" || r === "owner";
  const isEditor = (r: Role) => r !== "member";

  function me(role: Role) {
    return { user: { ...ROLE_USERS[role] }, csrfToken: "mock-csrf-token", sessionExpiresAt: "2026-12-28T10:00:00+09:00" };
  }

  return function respond(method: string, url: string, role: Role | null, body?: unknown): MockResponse {
    const u = new URL(url, "http://mock");
    const p = u.pathname.replace(/^\/api\/v1/, "");
    const q = u.searchParams;
    const b = (body ?? {}) as Record<string, unknown>;

    // 画像（アバター・物品の写真）。認証つきの配信を模して、ログイン中だけ返す
    if (p.startsWith("/media/") && method === "GET") {
      if (!role) return err(401, "UNAUTHENTICATED", "ログインしてください。");
      const hue = p.includes("camera") ? 200 : p.includes("vr") ? 280 : 20;
      return { status: 200, contentType: "image/svg+xml", raw: `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="300"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${hue},70%,62%)"/><stop offset="1" stop-color="hsl(${hue + 40},70%,40%)"/></linearGradient></defs><rect width="400" height="300" fill="url(#g)"/></svg>` };
    }

    // 公式サイトの画像（既存記事の /images/…。プレビューの中継先）。色つきの図で代用する
    if (p.startsWith("/web/images/") && method === "GET") {
      return { status: 200, contentType: "image/svg+xml", raw: `<svg xmlns="http://www.w3.org/2000/svg" width="600" height="400"><rect width="600" height="400" fill="hsl(150,45%,60%)"/><text x="300" y="210" font-size="28" text-anchor="middle" fill="#fff">site image</text></svg>` };
    }

    // 認証
    if (p === "/auth/login" && method === "POST") {
      const id = String(b.loginId ?? "");
      const r = (Object.keys(ROLE_USERS) as Role[]).find((k) => ROLE_USERS[k].loginId === id);
      if (!r) return err(401, "INVALID_CREDENTIALS", "ログインIDまたはパスワードが違います。（モック: owner / admin / editor / member のどれかを入力）");
      return { status: 200, json: me(r), setRole: r };
    }
    if (p === "/auth/logout" && method === "POST") return { status: 204, setRole: null };
    if ((p === "/auth/accept-invite" || p === "/auth/reset-password") && method === "POST") return { status: 204 };
    if (!role) return err(401, "UNAUTHENTICATED", "ログインしてください。");
    if (p === "/auth/me") return { status: 200, json: me(role) };

    if (opts.failPrefix && p.startsWith(opts.failPrefix)) return err(500, "INTERNAL", "サーバーでエラーが発生しました。（モックの失敗の再現）");
    const list = <T>(items: T[]) => (opts.empty ? [] : items);
    const forbidden = () => err(403, "FORBIDDEN", "この操作を行う権限がありません。");
    let m: RegExpMatchArray | null;

    // 管理（ユーザー）
    if (p.startsWith("/admin/") || p.startsWith("/owner/")) {
      if (p.startsWith("/owner/") ? role !== "owner" : !isAdmin(role)) return forbidden();
    }
    if (p === "/admin/users" && method === "GET") {
      const s = (q.get("q") ?? "").trim();
      return { status: 200, json: page(list(db.users.filter((x) => !s || x.loginId.includes(s) || x.displayName.includes(s)))) };
    }
    if (p === "/admin/users" && method === "POST") {
      const user = { id: `u-${Date.now()}`, loginId: String(b.loginId), displayName: String(b.displayName), status: "invited" as const, roles: ["member"], createdAt: db.now, activatedAt: null };
      db.users.push(user);
      return { status: 201, json: { url: "http://127.0.0.1/invite#token=mock-token", expiresAt: db.now, user: { id: user.id, loginId: user.loginId, displayName: user.displayName, status: user.status } } };
    }
    if ((m = p.match(/^\/admin\/users\/([^/]+)\/(invite-link|reset-link)$/))) return { status: 200, json: { url: `http://127.0.0.1/${m[2] === "invite-link" ? "invite" : "reset"}#token=mock-token`, expiresAt: db.now } };
    if ((m = p.match(/^\/admin\/users\/([^/]+)\/status$/))) {
      const x = db.users.find((y) => y.id === m![1]);
      if (x) (x as { status: string }).status = String(b.status);
      return { status: 200, json: x };
    }
    if ((m = p.match(/^\/(?:admin|owner)\/users\/([^/]+)\/(editor|admin)-role$/))) {
      const x = db.users.find((y) => y.id === m![1]);
      if (x) x.roles = b.enabled ? [...new Set([...x.roles, m[2]])] : x.roles.filter((r) => r !== m![2]);
      return { status: 200, json: x };
    }
    if ((m = p.match(/^\/admin\/users\/([^/]+)\/private-profile$/))) {
      const x = db.users.find((y) => y.id === m![1]) ?? db.users[0];
      if (method === "DELETE") return { status: 200, json: { consentWithdrawn: true, displayNameAnonymized: Boolean(b.anonymizeDisplayName), linksDeleted: 1, devicesDeleted: 1 } };
      return { status: 200, json: { id: x.id, loginId: x.loginId, displayName: x.displayName, private: db.profile.private } };
    }

    // プロフィール・メンバー
    if (p === "/me/profile" && method === "GET") return { status: 200, json: { ...db.profile, displayName: ROLE_USERS[role].displayName } };
    if (p === "/me/profile" && method === "PATCH") { Object.assign(db.profile, b); return { status: 200, json: db.profile }; }
    if (p === "/me/profile/visibility") { Object.assign(db.profile.visibility, b); return { status: 200, json: db.profile }; }
    if (p === "/me/profile/public-credit-consent") { db.profile.publicCreditConsent = { granted: Boolean(b.granted), grantedAt: b.granted ? db.now : null, withdrawnAt: b.granted ? null : db.now }; return { status: 200, json: db.profile.publicCreditConsent }; }
    if (p.startsWith("/me/")) return { status: method === "POST" ? 201 : method === "DELETE" ? 204 : 200, json: method === "DELETE" ? undefined : {} };
    if (p === "/members") {
      const s = (q.get("q") ?? "").trim();
      return { status: 200, json: page(list(db.members.filter((x) => !s || x.displayName.includes(s)).map(({ links, devices, works, ...rest }) => (void links, void devices, void works, rest)))) };
    }
    if ((m = p.match(/^\/members\/([^/]+)$/))) {
      const x = db.members.find((y) => y.id === m![1]);
      return x ? { status: 200, json: x } : err(404, "NOT_FOUND", "メンバーが見つかりません。");
    }

    // 物品
    const countCategories = () => { for (const c of db.categories) c.itemCount = db.items.filter((i) => i.categoryId === c.id && !i.archived).length; };
    if (p === "/inventory/categories" && method === "GET") { countCategories(); return { status: 200, json: { items: db.categories } }; }
    if (p.startsWith("/inventory/categories")) {
      if (!isAdmin(role)) return forbidden();
      countCategories();
      if (p === "/inventory/categories" && method === "POST") {
        const name = String(b.name ?? "").trim();
        if (!name) return err(422, "INVALID_INPUT", "カテゴリ名は1〜30文字で入力してください");
        if (db.categories.some((c) => c.name === name)) return err(409, "NAME_TAKEN", "同じ名前のカテゴリが既にあります");
        const c = { id: `cat-${Date.now()}`, name, sortOrder: Math.max(0, ...db.categories.map((x) => x.sortOrder)) + 10, itemCount: 0 };
        db.categories.push(c);
        return { status: 201, json: c };
      }
      if (p === "/inventory/categories/order" && method === "PUT") {
        const ids = (b.ids as string[]) ?? [];
        ids.forEach((id, i) => { const c = db.categories.find((x) => x.id === id); if (c) c.sortOrder = (i + 1) * 10; });
        db.categories.sort((x, y) => x.sortOrder - y.sortOrder);
        return { status: 200, json: { items: db.categories } };
      }
      if ((m = p.match(/^\/inventory\/categories\/([^/]+)$/))) {
        const c = db.categories.find((x) => x.id === m![1]);
        if (!c) return err(404, "NOT_FOUND", "見つかりません");
        if (method === "PATCH") {
          const name = String(b.name ?? "").trim();
          if (db.categories.some((x) => x.name === name && x.id !== c.id)) return err(409, "NAME_TAKEN", "同じ名前のカテゴリが既にあります");
          c.name = name;
          db.items.filter((i) => i.categoryId === c.id).forEach((i) => { i.category = name; });
          return { status: 200, json: c };
        }
        if (method === "DELETE") {
          if (db.categories.length <= 1) return err(409, "LAST_CATEGORY", "最後のカテゴリは削除できません");
          const inUse = db.items.filter((i) => i.categoryId === c.id);
          const to = db.categories.find((x) => x.id === q.get("moveTo") && x.id !== c.id);
          if (inUse.length && !to) return err(409, "IN_USE", `使われているカテゴリです（${inUse.length}件）。移し先のカテゴリを選んでください`);
          if (to) inUse.forEach((i) => { i.categoryId = to.id; i.category = to.name; });
          db.categories.splice(db.categories.indexOf(c), 1);
          return { status: 204 };
        }
      }
    }
    if (p === "/inventory/items" && method === "GET") {
      const s = (q.get("q") ?? "").trim(), cat = q.get("category"), catId = q.get("categoryId"), avail = q.get("available") === "true", mine = q.get("mine") === "true", sort = q.get("sort");
      const items = db.items.filter((x) => !x.archived && (!s || [x.name, x.location, x.category, x.description].some((v) => v.includes(s))) && (!cat || x.category === cat) && (!catId || x.categoryId === catId) && (!avail || x.available > 0) && (!mine || x.borrowedByMe > 0));
      if (sort === "available") items.sort((x, y) => y.available - x.available);
      countCategories();
      return { status: 200, json: { ...page(list(items)), categories: [...new Set(db.items.map((x) => x.category))], categoryList: db.categories } };
    }
    if (p === "/inventory/items" && method === "POST") {
      if (!isAdmin(role)) return forbidden();
      const cat = db.categories.find((c) => c.id === b.categoryId) ?? db.categories.find((c) => c.name === b.category) ?? db.categories[db.categories.length - 1];
      const it = { id: `i-${Date.now()}`, name: String(b.name), description: String(b.description ?? ""), category: cat.name, categoryId: cat.id, location: String(b.location ?? ""), mode: b.mode === "individual" ? "individual" as const : "quantity" as const, totalQuantity: Number(b.totalQuantity ?? 0), outstanding: 0, unitCount: 0, available: Number(b.totalQuantity ?? 0), archived: false, photoMediaId: null, borrowedByMe: 0 };
      db.items.push(it);
      return { status: 201, json: it };
    }
    if ((m = p.match(/^\/inventory\/items\/([^/]+)\/images$/)) && method === "PUT") {
      if (!isAdmin(role)) return forbidden();
      const ids = ((b.mediaIds as string[]) ?? []).slice(0, 8);
      db.images[m[1]] = ids;
      const it = db.items.find((x) => x.id === m![1]);
      if (it) it.photoMediaId = ids[0] ?? null;
      return { status: 200, json: { images: ids } };
    }
    if ((m = p.match(/^\/inventory\/items\/([^/]+)$/))) {
      const it = db.items.find((x) => x.id === m![1]);
      if (!it) return err(404, "NOT_FOUND", "物品が見つかりません。");
      if (method === "PATCH") {
        if (!isAdmin(role)) return forbidden();
        const cat = db.categories.find((c) => c.id === b.categoryId);
        if (cat) { it.categoryId = cat.id; it.category = cat.name; }
        const { categoryId, category, ...rest } = b; void categoryId; void category;
        Object.assign(it, rest);
        return { status: 200, json: it };
      }
      const loans = it.mode === "quantity" ? db.loans.filter((l) => l.itemId === it.id).map((l) => ({ id: l.id, borrowerDisplayName: l.borrowerDisplayName, quantity: l.quantity, borrowedAt: l.borrowedAt, elapsedDays: l.elapsedDays, mine: l.mine })) : [];
      return { status: 200, json: { ...it, images: db.images[it.id] ?? [], units: db.units[it.id] ?? [], loans } };
    }
    if (p === "/inventory/loans" && method === "GET") {
      const mine = q.get("mine") === "true";
      return { status: 200, json: page(list(db.loans.filter((l) => !mine || l.mine))) };
    }
    if (p === "/inventory/loans" && method === "POST") {
      const it = db.items.find((x) => x.id === b.itemId);
      const qty = Number(b.quantity ?? 1);
      if (!it || it.available < qty) return err(409, "INSUFFICIENT_STOCK", "借りられる数がありません。");
      it.available -= qty; it.outstanding += qty;
      const loan = { id: `lo-${Date.now()}`, itemId: it.id, itemName: it.name, assetTag: "", borrowerDisplayName: ROLE_USERS[role].displayName, quantity: qty, borrowedAt: db.now, elapsedDays: 0, mine: true };
      db.loans.unshift(loan);
      return { status: 201, json: { id: loan.id, quantity: qty, remaining: it.available } };
    }
    if ((m = p.match(/^\/inventory\/loans\/([^/]+)\/returns$/))) {
      const loan = db.loans.find((l) => l.id === m![1]);
      if (!loan) return err(404, "NOT_FOUND", "貸出が見つかりません。");
      const qty = Number(b.quantity ?? loan.quantity);
      loan.quantity -= qty;
      const it = db.items.find((x) => x.id === loan.itemId);
      if (it) { it.available += qty; it.outstanding -= qty; }
      if (loan.quantity <= 0) db.loans.splice(db.loans.indexOf(loan), 1);
      return { status: 200, json: { returned: qty, outstanding: Math.max(0, loan.quantity) } };
    }
    if (p === "/inventory/history") return { status: 200, json: page(list(db.history)) };
    if ((m = p.match(/^\/inventory\/items\/([^/]+)\/(units|photo)$/))) {
      if (!isAdmin(role)) return forbidden();
      return m[2] === "units" ? { status: 201, json: { id: `un-${Date.now()}`, assetTag: String(b.assetTag), serialNumber: b.serialNumber ?? null, state: "active", loaned: null } } : { status: 204 };
    }
    if (p === "/admin/inventory/corrections") return { status: 201, json: { replayed: false, itemOutstanding: 0, itemAvailable: 1 } };

    // 通知の設定
    if (p === "/admin/notifications/settings") return { status: 200, json: { items: db.notificationSettings } };
    if ((m = p.match(/^\/admin\/notifications\/settings\/(.+)$/))) {
      const s = db.notificationSettings.find((x) => x.eventType === decodeURIComponent(m![1]));
      if (s) Object.assign(s, { enabled: Boolean(b.enabled), discordChannelId: b.discordChannelId ? String(b.discordChannelId) : null });
      return { status: 200, json: s };
    }
    if (p === "/admin/notifications/test") return { status: 200, json: { ok: true } };
    if (p === "/admin/notifications/deliveries") return { status: 200, json: { items: list(db.deliveries.filter((d) => !q.get("status") || d.status === q.get("status"))) } };
    if (p.match(/^\/admin\/notifications\/deliveries\/[^/]+\/retry$/)) return { status: 204 };

    // CMS
    if (p === "/contents" && method === "GET") {
      const sec = q.get("section"), mine = q.get("mine") === "true", s = (q.get("q") ?? "").trim(), st = q.get("status");
      return { status: 200, json: page(list(db.contents.filter((c) => (!sec || c.section === sec) && (!mine || c.canEdit) && (!s || c.title.includes(s)) && (!st || (st === "published" ? c.isPublished : !c.isPublished))))) };
    }
    if (p === "/contents" && method === "POST") {
      const c = { id: `c-${Date.now()}`, type: b.type === "project" ? "project" as const : "article" as const, section: (b.section as "blog") ?? "blog", slug: String(b.slug), title: String(b.title), summary: "", revisionNo: 1, isPublished: false, canEdit: true, hasUnpublishedChanges: true, updatedAt: db.now,
        dateLabel: String((b.date as { label?: string } | undefined)?.label ?? ""), sortDate: ((b.date as { sortDate?: string | null } | undefined)?.sortDate ?? null), coverUrl: null, members: [db.contents[0].members[0]] };
      db.contents.unshift(c);
      return { status: 201, json: db.contentDetail(c) };
    }
    if ((m = p.match(/^\/contents\/([^/]+)(?:\/(revisions|publish|unpublish|collaborators|contributors))?$/))) {
      const c = db.contents.find((x) => x.id === m![1]);
      if (!c) return err(404, "NOT_FOUND", "記事が見つかりません。");
      const action = m[2];
      if ((action === "publish" || action === "unpublish") && !isEditor(role)) return forbidden();
      if (action === "publish") { c.isPublished = true; c.hasUnpublishedChanges = false; }
      if (action === "unpublish") c.isPublished = false;
      if (action === "revisions") { c.revisionNo += 1; c.title = String(b.title ?? c.title); c.hasUnpublishedChanges = true; return { status: 201, json: db.contentDetail(c) }; }
      if (action === "collaborators" && method === "PUT") {
        db.collabs[c.id] = ((b.userIds as string[]) ?? []).map((id) => { const u = db.members.find((x) => x.id === id); return { userId: id, displayName: u?.displayName ?? id, avatarMediaId: u?.avatarMediaId ?? null }; });
        return { status: 200, json: { collaborators: db.collabs[c.id] } };
      }
      if (action === "contributors" && method === "PUT") {
        db.contribs[c.id] = ((b.contributors as { userId: string; roleLabel: string }[]) ?? []).map((x) => { const u = db.members.find((y) => y.id === x.userId); return { userId: x.userId, displayName: u?.displayName ?? x.userId, avatarMediaId: u?.avatarMediaId ?? null, roleLabel: x.roleLabel }; });
        c.revisionNo += 1; c.hasUnpublishedChanges = true;
        return { status: 201, json: db.contentDetail(c) };
      }
      return { status: 200, json: db.contentDetail(c) };
    }
    if (p === "/media" && method === "POST") { const id = `m-uploaded-${++uploadSeq}`; return { status: 201, json: { id, publicUrl: `/api/v1/public/media/mockpublicid${String(uploadSeq).padStart(10, "0")}`, url: `/api/v1/media/${id}`, publicId: "mockpublicid0000000000", width: 400, height: 300, mimeType: "image/webp", byteSize: 1000, purpose: q.get("purpose") ?? "cms" } }; }

    // 既存記事の投稿者・公開版の確認
    if (p === "/admin/legacy-authors") return { status: 200, json: { items: list(db.legacyAuthors) } };
    if (p.startsWith("/admin/legacy-authors/")) return { status: 204 };
    if (p === "/admin/contents/published-search") return { status: 200, json: { items: list(db.publishedHits), truncated: false } };

    return err(404, "NOT_FOUND", `モックに未定義のAPIです: ${method} ${p}`);
  };
}
