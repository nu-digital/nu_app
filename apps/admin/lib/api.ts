// APIクライアント。/api/v1 は同一オリジン（Nextが中継）。CSRFトークンはメモリに保持する（Cookieは HttpOnly で読めない）。

export type Me = {
  user: { id: string; loginId: string; displayName: string; roles: string[] };
  csrfToken: string;
  sessionExpiresAt: string;
};

export type ApiError = { status: number; code: string; message: string; retryAfter?: number };

let csrfToken = "";

export function setCsrf(token: string) {
  csrfToken = token;
}

async function call(method: string, path: string, body?: unknown): Promise<{ status: number; json: unknown; retryAfter?: number }> {
  const headers: Record<string, string> = {};
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
  return { status: r.status, code: e?.code ?? "UNKNOWN", message: e?.message ?? "通信に失敗しました。もう一度お試しください。", retryAfter: r.retryAfter };
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
};

export type Member = {
  id: string;
  displayName: string;
  bio: string | null;
  faculty: string | null;
  department: string | null;
  schoolYear: number | null;
  links?: { id: string; label: string; url: string }[];
  devices?: { id: string; category: string; name: string; spec: string }[];
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
