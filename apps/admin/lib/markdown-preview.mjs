// CMSのプレビュー用Markdown描画の設定（docs/design/p3-cms.md §5）。
//
// **本当の防壁は、この描画時のサニタイズ**（rehype-raw の後に rehype-sanitize）。サーバーの検査（services/api/internal/mdcheck）は
// 「方針に反する入力を422で返す入口」で、パーサー（goldmark と remark/micromark/parse5）の差異をすり抜けるものがあり得るため、
// 下書きを描画する画面は必ずここを通す。許可リストはサーバーと同じ: 標準Markdown+GFM、生HTMLは details・summary・div(data-*)だけ、
// リンクは http・https・mailto・サイト内、画像は /images/ とアップロード画像(/api/v1/public/media/<22文字>)だけ。
//
// 許可・拒否の事例集 services/api/internal/mdcheck/testdata/corpus.json を、Go（mdcheck）とこのファイルのテストの両方で回す。
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import remarkGfm from "remark-gfm";

const YOUTUBE_ID = /^[A-Za-z0-9_-]{11}$/;
const MEDIA_SRC = /^\/api\/v1\/public\/media\/[A-Za-z0-9_-]{22}$/;

/** @type {import('hast-util-sanitize').Schema} */
export const sanitizeSchema = {
  ...defaultSchema,
  tagNames: [
    "h1", "h2", "h3", "h4", "h5", "h6", "p", "br", "hr", "blockquote", "pre", "code", "em", "strong", "del",
    "a", "img", "ul", "ol", "li", "input", "table", "thead", "tbody", "tfoot", "tr", "th", "td",
    "details", "summary", "div",
  ],
  attributes: {
    a: ["href"],
    img: ["src", "alt"],
    input: [["type", "checkbox"], ["disabled", true], "checked"],
    th: ["align"],
    td: ["align"],
    code: [["className", /^language-[\w-]+$/]],
    // 子なしの埋め込みの目印。値は検査済みのものだけ（YouTubeは11桁のID）
    div: [["dataYoutube", YOUTUBE_ID], ["dataVideo", "featured"], ["dataCarousel", "overview"]],
  },
  protocols: { href: ["http", "https", "mailto"], src: [] },
  required: { input: { type: "checkbox", disabled: true } },
  // ソース内のid/nameは許さない（属性自体を許可していない）
  clobber: [],
  ancestors: { li: ["ol", "ul"], tbody: ["table"], thead: ["table"], tfoot: ["table"], td: ["table"], th: ["table"], tr: ["table"] },
};

function hrefOk(v) {
  return /^https?:\/\//i.test(v) || /^mailto:/i.test(v) || /^#/.test(v) || (/^\/(?![/\\])/.test(v) && !/\.\./.test(v));
}
function srcOk(v) {
  return (/^\/images\/[^\s"'<>\\]+$/.test(v) && !v.includes("..")) || MEDIA_SRC.test(v);
}

/**
 * サニタイズの後に、URLをもう一段厳しく絞る（プロトコル相対`//`・バックスラッシュ・パス移動を落とす。
 * 画像は /images/ とアップロード画像だけ）。拒否したものは属性ごと外す。
 */
export function rehypeSafeUrls() {
  return (tree) => {
    const walk = (node) => {
      if (node.type === "element" && node.properties) {
        if (node.tagName === "a" && typeof node.properties.href === "string" && !hrefOk(node.properties.href)) delete node.properties.href;
        if (node.tagName === "img" && (typeof node.properties.src !== "string" || !srcOk(node.properties.src))) delete node.properties.src;
      }
      for (const c of node.children ?? []) walk(c);
    };
    walk(tree);
  };
}

/**
 * 子なしの埋め込みの目印（data-youtube / data-video / data-carousel）は、サーバーの検査と同じく**ちょうど1つ**だけ許す。
 * 2つ以上ある div は、目印をすべて外して属性なしの div にする（許可リストは各属性を個別に許すため、ここで揃える）。
 */
export function rehypeSingleDataAttr() {
  return (tree) => {
    const walk = (node) => {
      if (node.type === "element" && node.tagName === "div" && node.properties) {
        const keys = ["dataYoutube", "dataVideo", "dataCarousel"].filter((k) => node.properties[k] !== undefined);
        if (keys.length > 1) for (const k of keys) delete node.properties[k];
      }
      for (const c of node.children ?? []) walk(c);
    };
    walk(tree);
  };
}

export const remarkPlugins = [remarkGfm];
/** react-markdown と、テストのunifiedパイプラインで共通に使う（rehype-rawの後にサニタイズ）。 */
export const rehypePlugins = [rehypeRaw, [rehypeSanitize, sanitizeSchema], rehypeSafeUrls, rehypeSingleDataAttr];

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
