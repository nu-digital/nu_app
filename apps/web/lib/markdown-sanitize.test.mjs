// 公開サイトの描画時サニタイズのテスト（docs/design/p3-web.md §3）。
// (1) 既存67記事で、旧パイプライン（raw→ギャラリー）と新パイプライン（raw→サニタイズ→ギャラリー）のHTMLが完全一致（既存の見た目を変えない）
// (2) 共有の事例集と攻撃的な入力でも、出力は安全（adminのテストと同じ観点）
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import test from "node:test";
import rehypeRaw from "rehype-raw";
import rehypeStringify from "rehype-stringify";
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { unified } from "unified";
import { rehypeArticleGallery } from "./content-presentation.mjs";
import { sanitizePlugins } from "./markdown-sanitize.mjs";

const root = resolve(import.meta.dirname, "../../..");
const run = (md, plugins) =>
  String(unified().use(remarkParse).use(remarkGfm).use(remarkRehype, { allowDangerousHtml: true }).use(plugins).use(rehypeStringify).processSync(md));
// HTMLコメント（記事先頭の「元URL」）は、Reactの描画（react-markdown）では出力されない。比較ではコメントを除いた形で揃える
const noComments = (html) => html.replace(/<!--[\s\S]*?-->/g, "").replace(/\n{2,}/g, "\n").trim();
const legacy = (md) => noComments(run(md, [rehypeRaw, rehypeArticleGallery]));
const currentRaw = (md) => run(md, [rehypeRaw, ...sanitizePlugins, rehypeArticleGallery]);
const current = (md) => currentRaw(md);

function assertSafe(html, label) {
  assert.ok(!/<\s*(script|iframe|video|audio|object|embed|style|form|svg|math|base|meta|link|frame|frameset|template|noscript)\b/i.test(html), `${label}: 危険な要素: ${html.slice(0, 200)}`);
  assert.ok(!/\son[a-z]+\s*=/i.test(html), `${label}: イベント属性`);
  assert.ok(!/\sstyle\s*=/i.test(html), `${label}: style属性`);
  for (const m of html.matchAll(/\b(href|src)="([^"]*)"/g)) {
    const v = m[2].replace(/&#x26;/g, "&").replace(/&amp;/g, "&");
    if (m[1] === "href") assert.ok(/^https?:\/\//i.test(v) || /^mailto:/i.test(v) || /^#/.test(v) || /^\/(?![/\\])/.test(v), `${label}: href ${v}`);
    else assert.ok(/^\/images\/[^\s"'<>\\]+$/.test(v) || /^\/api\/v1\/public\/media\/[A-Za-z0-9_-]{22}$/.test(v), `${label}: src ${v}`);
  }
}

function bodies() {
  const dir = join(root, "apps/web/content");
  const out = [];
  for (const sec of readdirSync(dir)) {
    for (const f of readdirSync(join(dir, sec))) {
      let body = readFileSync(join(dir, sec, f), "utf8");
      if (body.startsWith("---\n")) body = body.slice(body.indexOf("\n---\n", 4) + 5);
      out.push([`${sec}/${f}`, body]);
    }
  }
  return out;
}

test("既存67記事: サニタイズを入れても、描画されるHTMLは旧パイプラインと完全に一致する", () => {
  const all = bodies();
  assert.equal(all.length, 67);
  for (const [name, body] of all) assert.equal(noComments(current(body)), legacy(body), `${name}: HTMLが変わった`);
});

test("共有の事例集と攻撃的な入力: 出力は安全", () => {
  const corpus = JSON.parse(readFileSync(join(root, "services/api/internal/mdcheck/testdata/corpus.json"), "utf8"));
  assert.ok(corpus.length >= 70);
  for (const c of corpus) assertSafe(current(c.markdown), c.name);
  for (const md of [
    "<img src=x onerror=alert(1)>", '<a href="javascript:alert(1)">x</a>', "<svg onload=alert(1)></svg>", '<iframe src="https://www.youtube.com/embed/ZEguS-om_Ec"></iframe>',
    "[x](//evil.example/x)", "![x](https://tracker.example/p.png)", "![x](/images/../../etc/passwd)", '<div data-youtube="ZEguS-om_Ec" onclick="alert(1)" style="x"></div>',
    '<div data-youtube="a" data-video="x" data-carousel="y"></div>', "<scr<script>ipt>alert(1)</scr</script>ipt>", "[x][r]\n\n[r]: javascript:alert(1)",
  ]) assertSafe(current(md), md);
});

test("サニタイズはギャラリー化の前: ギャラリーの属性（role・tabindex・aria-label）が残る", () => {
  const html = current("![猫](/images/a.webp)");
  assert.match(html, /<img src="\/images\/a\.webp" alt="猫" tabindex="0" role="button" aria-label="猫を拡大">/);
});

test("YouTubeの目印は11桁のIDだけ・目印は1つだけ", () => {
  assert.match(current('<div data-youtube="ZEguS-om_Ec"></div>'), /data-youtube="ZEguS-om_Ec"/);
  assert.ok(!current('<div data-youtube="short"></div>').includes("data-youtube"));
  assert.ok(!current('<div data-youtube="ZEguS-om_Ec" data-video="featured"></div>').includes("data-"));
});
