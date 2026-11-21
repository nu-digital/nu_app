// APIクライアント。/api/v1 は同一オリジン（Nextが中継）。CSRFトークンはメモリに保持する（Cookieは HttpOnly で読めない）。

export type Me = {
  user: { id: string; loginId: string; displayName: string; roles: string[] };
  csrfToken: string;
  sessionExpiresAt: string;
};

export type ApiError = { status: number; code: string; message: string; retryAfter?: number; body?: unknown };

let csrfToken = "";

export function setCsrf(token: string) {
  csrfToken = token;
}

async function call(method: string, path: string, body?: unknown, extraHeaders?: Record<string, string>): Promise<{ status: number; json: unknown; retryAfter?: number }> {
  const headers: Record<string, string> = { ...extraHeaders };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET" && csrfToken) headers["X-CSRF-Token"] = csrfToken;
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
    cache: "no-store",
  });
  let json: unknown = null;
  const text = await res.text();
  if (text) {
    try {
      json = JSON.parse(text);
    } catch {
      json = null;
    }
  }
  const ra = Number(res.headers.get("Retry-After"));
  return { status: res.status, json, retryAfter: Number.isFinite(ra) && ra > 0 ? ra : undefined };
}

function toError(r: { status: number; json: unknown; retryAfter?: number }): ApiError {
  const e = (r.json as { error?: { code?: string; message?: string } } | null)?.error;
  return { status: r.status, code: e?.code ?? "UNKNOWN", message: e?.message ?? "通信に失敗しました。もう一度お試しください。", retryAfter: r.retryAfter, body: r.json };
}

export async function fetchMe(): Promise<Me | null> {
  const r = await call("GET", "/auth/me");
  if (r.status === 401) return null;
  if (r.status !== 200) throw toError(r);
  const me = r.json as Me;
  setCsrf(me.csrfToken);
  return me;
}

export async function login(loginId: string, password: string): Promise<Me> {
  const r = await call("POST", "/auth/login", { loginId, password });
  if (r.status !== 200) throw toError(r);
  const me = r.json as Me;
  setCsrf(me.csrfToken);
  return me;
}

export async function logout(): Promise<void> {
  const r = await call("POST", "/auth/logout");
  if (r.status !== 204 && r.status !== 401) throw toError(r);
  setCsrf("");
}

export async function submitToken(kind: "accept-invite" | "reset-password", token: string, password: string): Promise<void> {
  const r = await call("POST", `/auth/${kind}`, { token, password });
  if (r.status !== 204) throw toError(r);
}

/** 利用者向けの文言へ変換する（サーバーの文言が日本語で短いものはそのまま使う）。 */
export function describe(e: unknown): string {
  const err = e as Partial<ApiError>;
  if (err?.status === 429) return "試行が多すぎます。しばらくしてからお試しください。";
  // Bot未設定の503は「混み合っている」ではなく、設定が必要という案内そのものを出す
  if (err?.code === "BOT_NOT_CONFIGURED" && err.message) return err.message;
  if (err?.status === 503) return "混み合っています。しばらくしてからお試しください。";
  if (err?.message) return err.message;
  return "通信に失敗しました。もう一度お試しください。";
}

// ---- 管理（ユーザー管理）。権限はAPIが強制する。画面側の出し分けは案内のためだけ。 ----

export type AdminUser = {
  id: string;
  loginId: string;
  displayName: string;
  status: "invited" | "active" | "disabled";
  roles: string[];
  createdAt: string;
  activatedAt: string | null;
};

export type LinkResult = { url: string; expiresAt: string; user?: { id: string; loginId: string; displayName: string; status: string } };

export async function listUsers(q: string, cursor?: string): Promise<{ items: AdminUser[]; nextCursor: string | null }> {
  const params = new URLSearchParams();
  if (q) params.set("q", q);
  if (cursor) params.set("cursor", cursor);
  params.set("limit", "50");
  const r = await call("GET", `/admin/users?${params.toString()}`);
  if (r.status !== 200) throw toError(r);
  return r.json as { items: AdminUser[]; nextCursor: string | null };
}

export async function createUser(loginId: string, displayName: string): Promise<LinkResult> {
  const r = await call("POST", "/admin/users", { loginId, displayName });
  if (r.status !== 201) throw toError(r);
  return r.json as LinkResult;
}

export async function issueLink(userId: string, kind: "invite-link" | "reset-link"): Promise<LinkResult> {
  const r = await call("POST", `/admin/users/${userId}/${kind}`);
  if (r.status !== 200) throw toError(r);
  return r.json as LinkResult;
}

export async function setUserStatus(userId: string, status: "disabled" | "active"): Promise<void> {
  const r = await call("PATCH", `/admin/users/${userId}/status`, { status });
  if (r.status !== 200) throw toError(r);
}

export async function setRole(userId: string, role: "editor" | "admin", enabled: boolean): Promise<void> {
  const path = role === "editor" ? `/admin/users/${userId}/editor-role` : `/owner/users/${userId}/admin-role`;
  const r = await call("PUT", path, { enabled });
  if (r.status !== 200) throw toError(r);
}

// ---- プロフィール・メンバー ----

export type Visibility = "private" | "members";

export type MyProfile = {
  displayName: string;
  bio: string | null;
  faculty: string | null;
  department: string | null;
  schoolYear: number | null;
  visibility: Record<"bio" | "faculty" | "department" | "schoolYear", Visibility>;
  private: { legalName: string | null; studentNumber: string | null; universityEmail: string | null; nearestStation: string | null };
  links: { id: string; label: string; url: string; sortOrder: number; visibility: Visibility }[];
  devices: { id: string; category: string; name: string; spec: string; sortOrder: number; visibility: Visibility }[];
  publicCreditConsent: { granted: boolean; grantedAt: string | null; withdrawnAt: string | null };
  /** 制作実績（公開版が本人をクレジットしている作品）。 */
  works: Work[];
};
export type Work = { section: string; slug: string; title: string; roles: string[] };

export type Member = {
  id: string;
  displayName: string;
  bio: string | null;
  faculty: string | null;
  department: string | null;
  schoolYear: number | null;
  links?: { id: string; label: string; url: string }[];
  devices?: { id: string; category: string; name: string; spec: string }[];
  /** 制作実績。本人の公開クレジットの同意が続いているあいだだけ、他のメンバーに見える。 */
  works?: Work[];
};

async function ok<T>(r: { status: number; json: unknown; retryAfter?: number }, ...good: number[]): Promise<T> {
  if (!good.includes(r.status)) throw toError(r);
  return r.json as T;
}

export const getMyProfile = async () => ok<MyProfile>(await call("GET", "/me/profile"), 200);
export const patchMyProfile = async (body: Record<string, unknown>) => ok<MyProfile>(await call("PATCH", "/me/profile", body), 200);
export const putVisibility = async (body: Record<string, Visibility>) => ok<MyProfile>(await call("PUT", "/me/profile/visibility", body), 200);
export const putConsent = async (granted: boolean) => ok<MyProfile["publicCreditConsent"]>(await call("PUT", "/me/profile/public-credit-consent", { granted }), 200);
export const addLink = async (body: { label: string; url: string; visibility: Visibility }) => ok<unknown>(await call("POST", "/me/profile/links", body), 201);
export const patchLink = async (id: string, body: Record<string, unknown>) => ok<unknown>(await call("PATCH", `/me/profile/links/${id}`, body), 200);
export const deleteLink = async (id: string) => ok<unknown>(await call("DELETE", `/me/profile/links/${id}`), 204);
export const addDevice = async (body: { category: string; name: string; spec: string; visibility: Visibility }) => ok<unknown>(await call("POST", "/me/devices", body), 201);
export const patchDevice = async (id: string, body: Record<string, unknown>) => ok<unknown>(await call("PATCH", `/me/devices/${id}`, body), 200);
export const deleteDevice = async (id: string) => ok<unknown>(await call("DELETE", `/me/devices/${id}`), 204);

export async function listMembers(q: string, cursor?: string) {
  const p = new URLSearchParams({ limit: "50" });
  if (q) p.set("q", q);
  if (cursor) p.set("cursor", cursor);
  return ok<{ items: Member[]; nextCursor: string | null }>(await call("GET", `/members?${p.toString()}`), 200);
}
export const getMember = async (id: string) => ok<Member>(await call("GET", `/members/${id}`), 200);

export type PrivateProfile = { id: string; loginId: string; displayName: string; private: MyProfile["private"] };
export const getPrivateProfile = async (id: string) => ok<PrivateProfile>(await call("GET", `/admin/users/${id}/private-profile`), 200);
export const deletePrivateProfile = async (id: string, reason: string, anonymizeDisplayName: boolean) =>
  ok<{ consentWithdrawn: boolean; displayNameAnonymized: boolean; linksDeleted: number; devicesDeleted: number }>(
    await call("DELETE", `/admin/users/${id}/private-profile`, { reason, anonymizeDisplayName }), 200);

// ---- 物品 ----

export type Item = {
  id: string; name: string; description: string; category: string; location: string; mode: "quantity" | "individual";
  totalQuantity: number; outstanding: number; unitCount: number; available: number; archived?: boolean;
};
export type ItemUnit = { id: string; assetTag: string; serialNumber: string | null; state: string; loaned: { borrowerDisplayName: string; since: string; elapsedDays: number } | null };
export type ItemLoan = { id: string; borrowerDisplayName: string; quantity: number; borrowedAt: string; elapsedDays: number };
export type ItemDetail = Item & { units: ItemUnit[]; loans: ItemLoan[] };
export type OutstandingLoan = { id: string; itemId: string; itemName: string; assetTag: string; borrowerDisplayName: string; quantity: number; borrowedAt: string; elapsedDays: number; mine: boolean };
export type HistoryEvent = {
  id: string; kind: "loan" | "return" | "correction"; itemId: string; itemName: string; assetTag: string; borrowerDisplayName: string; actorDisplayName: string;
  quantity: number; at: string; correctionKind?: string; targetType?: string; reason?: string;
};

export async function listItems(p: { q?: string; category?: string; available?: boolean; cursor?: string }) {
  const params = new URLSearchParams({ limit: "50" });
  if (p.q) params.set("q", p.q);
  if (p.category) params.set("category", p.category);
  if (p.available) params.set("available", "true");
  if (p.cursor) params.set("cursor", p.cursor);
  return ok<{ items: Item[]; nextCursor: string | null; categories: string[] }>(await call("GET", `/inventory/items?${params.toString()}`), 200);
}
export const getItem = async (id: string) => ok<ItemDetail>(await call("GET", `/inventory/items/${id}`), 200);
export const createLoan = async (body: { itemId: string; quantity?: number; unitId?: string }) =>
  ok<{ id: string; quantity: number; remaining: number }>(await call("POST", "/inventory/loans", body), 201);
export const returnLoan = async (loanId: string, quantity?: number) =>
  ok<{ returned: number; outstanding: number }>(await call("POST", `/inventory/loans/${loanId}/returns`, quantity === undefined ? undefined : { quantity }), 200);
export async function listLoans(p: { mine?: boolean; q?: string; cursor?: string }) {
  const params = new URLSearchParams({ limit: "50" });
  if (p.mine) params.set("mine", "true");
  if (p.q) params.set("q", p.q);
  if (p.cursor) params.set("cursor", p.cursor);
  return ok<{ items: OutstandingLoan[]; nextCursor: string | null }>(await call("GET", `/inventory/loans?${params.toString()}`), 200);
}
export async function listHistory(cursor?: string) {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor) params.set("cursor", cursor);
  return ok<{ items: HistoryEvent[]; nextCursor: string | null }>(await call("GET", `/inventory/history?${params.toString()}`), 200);
}

// --- 物品の管理（Admin/Owner。権限はAPIが強制する）---
export type ItemInput = { name: string; description: string; category: string; location: string };
export async function createItem(b: ItemInput & { mode: "quantity" | "individual"; totalQuantity?: number }) {
  return ok<Item>(await call("POST", "/inventory/items", b), 201);
}
export async function updateItem(id: string, b: Partial<ItemInput> & { totalQuantity?: number; reason?: string; archived?: boolean }) {
  return ok<Item>(await call("PATCH", `/inventory/items/${id}`, b), 200);
}
export async function createUnit(itemId: string, assetTag: string, serialNumber?: string) {
  return ok<ItemUnit>(await call("POST", `/inventory/items/${itemId}/units`, { assetTag, ...(serialNumber ? { serialNumber } : {}) }), 201);
}
/** 訂正。同じ`key`の再送は二重適用されない（フォームを開くたびに新しいキーを作る）。 */
export async function createCorrection(key: string, b: { targetType: "loan" | "return"; targetId: string; kind: "adjust" | "void"; deltaQuantity?: number; reason: string }) {
  const r = await call("POST", "/admin/inventory/corrections", b, { "Idempotency-Key": key });
  if (r.status !== 200 && r.status !== 201) throw toError(r);
  return r.json as { replayed: boolean; itemOutstanding: number; itemAvailable: number };
}

// --- 通知の設定（Admin/Owner。権限はAPIが強制する）---
export type NotificationSetting = { eventType: string; label: string; enabled: boolean; discordChannelId: string | null; updatedAt: string };
export type Delivery = { id: string; eventType: string; label: string; status: "pending" | "sent" | "dead"; attempts: number; nextAttemptAt: string; lastError: string | null; createdAt: string; sentAt: string | null };
export const listNotificationSettings = async () => ok<{ items: NotificationSetting[] }>(await call("GET", "/admin/notifications/settings"), 200);
export const putNotificationSetting = async (eventType: string, enabled: boolean, discordChannelId: string) =>
  ok<NotificationSetting>(await call("PUT", `/admin/notifications/settings/${encodeURIComponent(eventType)}`, { enabled, discordChannelId }), 200);
export const testNotification = async (discordChannelId: string) => ok<{ ok: boolean }>(await call("POST", "/admin/notifications/test", { discordChannelId }), 200);
export const listDeliveries = async (status?: string) =>
  ok<{ items: Delivery[] }>(await call("GET", `/admin/notifications/deliveries?limit=50${status ? `&status=${status}` : ""}`), 200);
export async function retryDelivery(id: string): Promise<void> {
  const r = await call("POST", `/admin/notifications/deliveries/${id}/retry`);
  if (r.status !== 204) throw toError(r);
}

// --- CMS（記事・作品）。権限・検査はAPIが強制する ---
export type ContentListItem = {
  id: string; type: "article" | "project"; section: "blog" | "news" | "project"; slug: string; title: string; summary: string;
  revisionNo: number; isPublished: boolean; canEdit: boolean; hasUnpublishedChanges: boolean; updatedAt: string;
};
export type ContentDate = { label: string; kind: "day" | "month" | "range" | "unknown"; sortDate: string | null; sortDateIsEstimated: boolean };
export type MediaInfo = { id: string; publicUrl: string; url: string; publicId: string; width: number; height: number; mimeType: string; byteSize: number; purpose: string };
export type ContentRevision = {
  revisionNo: number; title: string; summary: string; markdown: string; metadata: Record<string, string>; date: ContentDate;
  editedBy: string; createdAt: string; coverMedia: MediaInfo | null; contributors?: { userId: string; displayName: string; roleLabel: string }[];
};
export type ContentDetail = {
  id: string; type: "article" | "project"; section: "blog" | "news" | "project"; slug: string;
  creator: { userId: string; displayName: string }; workingRevision: ContentRevision; publishedRevisionNo: number | null;
  collaborators: { userId: string; displayName: string }[]; revisions?: { revisionNo: number; title: string; editedBy: string; createdAt: string }[];
  canManageCollaborators: boolean; canPublish: boolean; createdAt: string; updatedAt: string;
};
export type RevisionInput = {
  baseRevision: number; title: string; summary: string; markdown: string;
  metadata: Record<string, string | null>; date: { label: string; kind: string; sortDate: string | null };
  coverMediaId?: string | null;
};
export async function listContents(p: { section?: string; mine?: boolean; cursor?: string }) {
  const params = new URLSearchParams({ limit: "50" });
  if (p.section) params.set("section", p.section);
  if (p.mine) params.set("mine", "true");
  if (p.cursor) params.set("cursor", p.cursor);
  return ok<{ items: ContentListItem[]; nextCursor: string | null }>(await call("GET", `/contents?${params.toString()}`), 200);
}
export const createContent = async (b: { type: string; section: string; slug: string; title: string; markdown: string }) =>
  ok<ContentDetail>(await call("POST", "/contents", b), 201);
export const getContent = async (id: string, revision?: number) =>
  ok<ContentDetail>(await call("GET", `/contents/${id}${revision ? `?revision=${revision}` : ""}`), 200);
/** 版を保存する。他の人が先に保存していたら、ApiError（code=REVISION_CONFLICT、body.current＝現在の作業版）を投げる。 */
export async function saveRevision(id: string, b: RevisionInput): Promise<{ content: ContentDetail; changed: boolean }> {
  const r = await call("POST", `/contents/${id}/revisions`, b);
  if (r.status !== 200 && r.status !== 201) throw toError(r);
  return { content: r.json as ContentDetail, changed: r.status === 201 };
}
export const publishContent = async (id: string, revisionNo: number) => ok<ContentDetail>(await call("POST", `/contents/${id}/publish`, { revisionNo }), 200);
export const unpublishContent = async (id: string) => ok<ContentDetail>(await call("POST", `/contents/${id}/unpublish`), 200);
export function conflictRevision(e: unknown): ContentRevision | null {
  const err = e as Partial<ApiError>;
  if (err?.code !== "REVISION_CONFLICT") return null;
  return ((err.body as { current?: ContentRevision } | undefined)?.current) ?? null;
}

/** 画像をアップロードする（生のバイト列。形式・大きさ・寸法の検査と再エンコードはAPIが行う）。 */
export async function uploadMedia(file: File): Promise<MediaInfo> {
  const ct = file.type === "image/jpeg" || file.type === "image/png" || file.type === "image/webp" ? file.type : "application/octet-stream";
  const headers: Record<string, string> = { "Content-Type": ct };
  if (csrfToken) headers["X-CSRF-Token"] = csrfToken;
  const res = await fetch("/api/v1/media?purpose=cms", { method: "POST", headers, body: file, credentials: "same-origin", cache: "no-store" });
  let json: unknown = null;
  try {
    json = JSON.parse(await res.text());
  } catch {
    json = null;
  }
  const ra = Number(res.headers.get("Retry-After"));
  if (res.status !== 201) throw toError({ status: res.status, json, retryAfter: Number.isFinite(ra) && ra > 0 ? ra : undefined });
  return json as MediaInfo;
}

export const putCollaborators = async (id: string, userIds: string[]) =>
  ok<{ collaborators: { userId: string; displayName: string }[] }>(await call("PUT", `/contents/${id}/collaborators`, { userIds }), 200);

/** 制作メンバーを置き換える（新しい版ができる）。baseRevisionは編集の元にした版の番号。 */
export async function putContributors(id: string, baseRevision: number, contributors: { userId: string; roleLabel: string }[]): Promise<ContentDetail> {
  const r = await call("PUT", `/contents/${id}/contributors`, { baseRevision, contributors });
  if (r.status !== 200 && r.status !== 201) throw toError(r);
  return r.json as ContentDetail;
}

// ---- 既存記事の投稿者（D14） ----

export type LegacyAuthor = {
  id: string; name: string; publicationState: "unconfirmed" | "permitted" | "withdrawn"; confirmedAt: string | null; withdrawnAt: string | null;
  erased: boolean; linkedUserId: string | null; linkedDisplayName: string | null; contentCount: number;
};
export const listLegacyAuthors = async () => ok<{ items: LegacyAuthor[] }>(await call("GET", "/admin/legacy-authors"), 200);
export const permitLegacyAuthor = async (id: string, note: string) => { await ok<unknown>(await call("POST", `/admin/legacy-authors/${id}/permit`, { note }), 204); };
export const withdrawLegacyAuthor = async (id: string) => { await ok<unknown>(await call("POST", `/admin/legacy-authors/${id}/withdraw`), 204); };
export const linkLegacyAuthor = async (id: string, userId: string) => { await ok<unknown>(await call("PUT", `/admin/legacy-authors/${id}/link`, { userId }), 204); };
export const unlinkLegacyAuthor = async (id: string) => { await ok<unknown>(await call("DELETE", `/admin/legacy-authors/${id}/link`), 204); };
