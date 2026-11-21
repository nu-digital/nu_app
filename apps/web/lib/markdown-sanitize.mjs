// Markdownの描画時サニタイズの許可リスト（docs/design/p3-cms.md §5、p3-web.md §3）。
//
// **このファイルは apps/web/lib と apps/admin/lib で、バイト単位で同一でなければならない**（scripts/check.mjs が検査する）。
// 公開サイトと管理画面のプレビューが別々の許可リストを持つと、プレビューで安全だったものが公開で通る／その逆が起きるため。
// 本当の防壁は、この描画時のサニタイズ（rehype-raw の後に rehype-sanitize）。サーバーの検査（services/api/internal/mdcheck）は
// 「方針に反する入力を422で返す入口」で、パーサーの差異をすり抜けるものがあり得る。
// 許可: 標準Markdown+GFM、生HTMLは details・summary・div(data-*)だけ、リンクは http・https・mailto・サイト内、
// 画像は /images/ とアップロード画像(/api/v1/public/media/<22文字>)だけ。
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";

export const YOUTUBE_ID = /^[A-Za-z0-9_-]{11}$/;
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

/** rehype-raw の後に、この並びで適用する（ギャラリー化などの装飾は、これらの後に行う）。 */
export const sanitizePlugins = [[rehypeSanitize, sanitizeSchema], rehypeSafeUrls, rehypeSingleDataAttr];
