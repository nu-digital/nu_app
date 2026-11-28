import { previewUrlTransform } from "./markdown-preview.mjs";

/** ホームに出す、公開中の記事・作品（公開API `GET /public/contents` の1件）。 */
export type PublicItem = {
  type: "article" | "project"; section: "blog" | "news" | "project"; slug: string; title: string; summary: string; author: string;
  metadata: Record<string, string>; date: { label: string; kind: string; sortDate: string | null }; coverUrl?: string;
};

export type Slide = { key: string; image: string; title: string; kind: "記事" | "作品"; dateLabel: string; href: string | null };

/** 公開サイトのURLの土台。ポータルが `app.` で始まるホストなら、その下のホスト（本番: app.nu-digital.jp → nu-digital.jp）。開発中（localhost等）は null。 */
export function publicSiteBase(hostname: string): string | null {
  return hostname.startsWith("app.") && hostname.length > 6 ? `https://${hostname.slice(4)}` : null;
}

export const publicHref = (it: Pick<PublicItem, "section" | "slug">, base: string | null) => (base ? `${base}/${it.section}/${it.slug}/` : null);

/**
 * 画像の表示URL。公開サイトと同じ優先順位（既存記事の画像 metadata.image → アップロードしたカバー）。
 * 既存記事の画像（/images/…）は公式サイト側のファイルなので、ポータルでは中継（/web-images/…）で表示する。公開API（匿名で読める公開版）だけが元なので、非公開の画像は出ない。
 */
export function imageOf(it: Pick<PublicItem, "metadata" | "coverUrl">): string | null {
  const legacy = it.metadata?.image;
  if (legacy && legacy.startsWith("/images/")) return previewUrlTransform(legacy);
  return it.coverUrl ?? null;
}

/** メインビジュアルのスライド。新しい順（受け取った順）に、画像のあるものだけを最大 max 件。 */
export function heroSlides(items: PublicItem[], base: string | null, max = 6): Slide[] {
  const out: Slide[] = [];
  for (const it of items) {
    const image = imageOf(it);
    if (!image) continue;
    out.push({ key: `${it.section}/${it.slug}`, image, title: it.title, kind: it.type === "project" ? "作品" : "記事", dateLabel: it.date?.label ?? "", href: publicHref(it, base) });
    if (out.length >= max) break;
  }
  return out;
}

/** 種類ごとの最近の公開（新しい順に max 件） */
export const recentOf = (items: PublicItem[], type: PublicItem["type"], max = 4) => items.filter((i) => i.type === type).slice(0, max);
