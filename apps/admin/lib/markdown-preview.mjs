// CMSのプレビュー用Markdown描画の設定（docs/design/p3-cms.md §5）。
//
// **本当の防壁は、この描画時のサニタイズ**（rehype-raw の後に rehype-sanitize）。サーバーの検査（services/api/internal/mdcheck）は
// 「方針に反する入力を422で返す入口」で、パーサー（goldmark と remark/micromark/parse5）の差異をすり抜けるものがあり得るため、
// 下書きを描画する画面は必ずここを通す。許可リストはサーバーと同じ: 標準Markdown+GFM、生HTMLは details・summary・div(data-*)だけ、
// リンクは http・https・mailto・サイト内、画像は /images/ とアップロード画像(/api/v1/public/media/<22文字>)だけ。
//
// 許可・拒否の事例集 services/api/internal/mdcheck/testdata/corpus.json を、Go（mdcheck）とこのファイルのテストの両方で回す。
import rehypeRaw from "rehype-raw";
import remarkGfm from "remark-gfm";

import { sanitizePlugins, sanitizeSchema, rehypeSafeUrls, rehypeSingleDataAttr, YOUTUBE_ID } from "./markdown-sanitize.mjs";

export { sanitizeSchema, rehypeSafeUrls, rehypeSingleDataAttr };

export const remarkPlugins = [remarkGfm];
/** react-markdown と、テストのunifiedパイプラインで共通に使う（rehype-rawの後にサニタイズ）。 */
export const rehypePlugins = [rehypeRaw, ...sanitizePlugins];

/**
 * プレビュー専用のURL変換。下書きの画像は公開されるまで匿名には配信されないため、
 * 本文のアップロード画像URL（/api/v1/public/media/<public_id>）を、認証つきのプレビュー経路へ読み替えて表示する。
 * サニタイズの後に適用されるので、許可されたURLだけがここへ来る。
 */
export function previewUrlTransform(url) {
  const m = /^\/api\/v1\/public\/media\/([A-Za-z0-9_-]{22})$/.exec(url);
  return m ? `/api/v1/media/by-public/${m[1]}` : url;
}

/** 本文のYouTube URLから動画IDを取り出す（既存サイトのlib/youtube.tsと同じ許可ホスト・11桁ID。D10）。 */
export function youtubeIds(text) {
  const ids = new Set();
  for (const match of text.matchAll(/https?:\/\/[^\s<>"'\])]+/g)) {
    try {
      const url = new URL(match[0].replace(/&amp;/g, "&"));
      const host = url.hostname.toLowerCase();
      let id = null;
      if (host === "youtu.be") id = url.pathname.split("/")[1];
      if (["youtube.com", "www.youtube.com", "m.youtube.com", "youtube-nocookie.com", "www.youtube-nocookie.com"].includes(host)) {
        id = url.pathname === "/watch" ? url.searchParams.get("v") : url.pathname.match(/^\/(?:embed|shorts|live)\/([^/]+)/)?.[1] ?? null;
      }
      if (id && YOUTUBE_ID.test(id)) ids.add(id);
    } catch {
      // 不正なURLは無視する
    }
  }
  return [...ids];
}
