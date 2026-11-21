import { cache } from "react";
import { articleCategory, articleDateOrder } from "./content-presentation.mjs";

// 公開コンテンツは、公開API（Go）から取得する（docs/design/p3-web.md §3）。
//   - サーバー側のみ（API_INTERNAL_URL）。Cookie等の認証情報は一切送らない。
//   - キャッシュしない（cache: "no-store"）。撤回・個人情報削除（D08）を、次のリクエストから反映するため。
//   - 取得全体に1つの期限（5秒）。404はnull、5xx・通信失敗は例外（app/error.tsx が5xxのno-storeで返す）。

export type ContentCollection = "blog" | "news" | "project";

export type ContentMeta = {
  collection: ContentCollection;
  slug: string;
  title: string;
  image: string;
  author: string;
  date: string;
  category: string;
  excerpt: string;
};

export type Contributor = { displayName: string; roleLabel: string };

export type ContentPost = ContentMeta & {
  content: string;
  contributors: Contributor[];
};

type ApiItem = {
  section: ContentCollection;
  slug: string;
  title: string;
  summary: string;
  author: string;
  metadata: Record<string, string>;
  date: { label: string; sortDate: string | null };
  coverUrl?: string;
};
type ApiDetail = ApiItem & { markdown: string; contributors: Contributor[] };

const API_TIMEOUT_MS = 5000;
const SLUG_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$/;
const COLLECTIONS: ContentCollection[] = ["blog", "news", "project"];

function apiBase() {
  return (process.env.API_INTERNAL_URL ?? "http://127.0.0.1:8080").replace(/\/$/, "");
}

async function apiGet(path: string, signal: AbortSignal): Promise<Response> {
  return fetch(`${apiBase()}/api/v1${path}`, {
    cache: "no-store",
    credentials: "omit",
    headers: { accept: "application/json" },
    signal,
    redirect: "error",
  });
}

function toMeta(item: ApiItem): ContentMeta {
  return {
    collection: item.section,
    slug: item.slug,
    title: item.title,
    image: item.metadata.image ?? item.coverUrl ?? "/images/ogp/member.webp",
    author: item.author,
    date: item.date.label,
    category: articleCategory(item.section, item.metadata.category ?? ""),
    // 本文へのフォールバックはしない（既存の空の抜粋は空のまま）
    excerpt: item.metadata.excerpt ?? item.summary ?? "",
  };
}

/** 全件（3区分）を取得する。同じリクエスト内では1回だけ。 */
const fetchAll = cache(async (): Promise<ContentMeta[]> => {
  const signal = AbortSignal.timeout(API_TIMEOUT_MS); // 取得全体で1つの期限（ページ数ぶん延びない）
  const out: ContentMeta[] = [];
  let cursor = "";
  for (let page = 0; page < 50; page++) {
    const res = await apiGet(`/public/contents?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, signal);
    if (!res.ok) throw new Error(`公開APIが ${res.status} を返しました`);
    const body = (await res.json()) as { items: ApiItem[]; nextCursor?: string | null };
    for (const item of body.items) out.push(toMeta(item));
    if (!body.nextCursor) return out;
    cursor = body.nextCursor;
  }
  throw new Error("公開APIのページ数が多すぎます");
});

// 並びは、以前（ファイル読み込み）と同じ規則: ファイル名（slug + ".md"）の昇順で取り込んでから、開始日の降順の安定ソート。
// ".md" を付けて比べるのが肝心（"demo" と "demo-yura" は、ファイル名では "demo-yura.md" が先。以前の並びを変えない）
const fileName = (p: ContentMeta) => `${p.slug}.md`;
const bySlug = (a: ContentMeta, b: ContentMeta) => (fileName(a) < fileName(b) ? -1 : fileName(a) > fileName(b) ? 1 : 0);
// 期間だけの記録は、記載された開始の月・日を並び順にだけ使う（sortDateは使わず、表示の日付は原文のまま）
const chronology = (post: ContentMeta) => articleDateOrder(post.date);

export async function getAllContent(collection: ContentCollection): Promise<ContentMeta[]> {
  const all = await fetchAll();
  return all
    .filter((p) => p.collection === collection)
    .sort(bySlug)
    .sort((a, b) => chronology(b) - chronology(a));
}

export async function getLatestContent(collection: ContentCollection, limit: number) {
  return (await getAllContent(collection)).slice(0, limit);
}

/** 3区分の新着順（同じ日は slug の localeCompare）。 */
export async function getBlogFeed(limit?: number): Promise<ContentMeta[]> {
  const posts = (await Promise.all(COLLECTIONS.map((c) => getAllContent(c)))).flat()
    .sort((a, b) => chronology(b) - chronology(a) || a.slug.localeCompare(b.slug));
  return limit === undefined ? posts : posts.slice(0, limit);
}

export const getContentBySlug = cache(async (collection: ContentCollection, slug: string): Promise<ContentPost | null> => {
  if (!SLUG_RE.test(slug)) return null;
  const signal = AbortSignal.timeout(API_TIMEOUT_MS);
  const res = await apiGet(`/public/contents/${collection}/${encodeURIComponent(slug)}`, signal);
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`公開APIが ${res.status} を返しました`);
  const item = (await res.json()) as ApiDetail;
  return { ...toMeta(item), content: item.markdown, contributors: item.contributors ?? [] };
});

export const getAllPosts = () => getAllContent("blog");
export const getLatestPosts = (limit: number) => getLatestContent("blog", limit);
export const getPostBySlug = (slug: string) => getContentBySlug("blog", slug);
