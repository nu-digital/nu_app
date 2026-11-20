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
