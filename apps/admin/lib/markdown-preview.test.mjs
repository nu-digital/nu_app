// プレビューの描画時サニタイズのテスト。サーバー（mdcheck）と共有する事例集と、既存67記事を通す。
// 実行: npm test --prefix apps/admin
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import test from "node:test";
import rehypeStringify from "rehype-stringify";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { unified } from "unified";
import { previewUrlTransform, rehypePlugins, remarkPlugins, youtubeIds } from "./markdown-preview.mjs";

const root = resolve(import.meta.dirname, "../../..");

function render(md) {
  return String(
    unified()
      .use(remarkParse)
      .use(remarkPlugins)
      .use(remarkRehype, { allowDangerousHtml: true })
      .use(rehypePlugins)
      .use(rehypeStringify)
      .processSync(md),
  );
}

// どんな入力でも、出力が満たすべき安全性
function assertSafe(html, label) {
  assert.ok(!/<\s*(script|iframe|video|audio|object|embed|style|form|svg|math|base|meta|link|frame|frameset|template|noscript)\b/i.test(html), `${label}: 危険な要素が残っている: ${html.slice(0, 200)}`);
  assert.ok(!/\son[a-z]+\s*=/i.test(html), `${label}: イベント属性が残っている: ${html.slice(0, 200)}`);
  assert.ok(!/\sstyle\s*=/i.test(html), `${label}: style属性が残っている`);
  assert.ok(!/\s(class|id|name)\s*=/i.test(html.replace(/class="language-[\w-]+"/g, "")), `${label}: class/id/name属性が残っている`);
  for (const m of html.matchAll(/\b(href|src)="([^"]*)"/g)) {
    const [, attr, val] = m;
    const v = val.replace(/&#x26;/g, "&").replace(/&amp;/g, "&");
    if (attr === "href") {
      assert.ok(/^https?:\/\//i.test(v) || /^mailto:/i.test(v) || /^#/.test(v) || /^\/(?![/\\])/.test(v), `${label}: 許可されないhref: ${v}`);
    } else {
      assert.ok(/^\/images\/[^\s"'<>\\]+$/.test(v) || /^\/api\/v1\/public\/media\/[A-Za-z0-9_-]{22}$/.test(v), `${label}: 許可されないsrc: ${v}`);
    }
    assert.ok(!/^\s*(javascript|data|vbscript|file):/i.test(v), `${label}: 危険なスキーム: ${v}`);
  }
  for (const m of html.matchAll(/<div([^>]*)>/g)) {
    const attrs = m[1].trim();
    assert.ok(attrs === "" || /^data-(youtube="[A-Za-z0-9_-]{11}"|video="featured"|carousel="overview")$/.test(attrs), `${label}: 許可されないdivの属性: ${attrs}`);
  }
}

const corpus = JSON.parse(readFileSync(join(root, "services/api/internal/mdcheck/testdata/corpus.json"), "utf8"));

test("共有の事例集: 許可・拒否のどちらの入力でも、描画の出力は安全", () => {
  assert.ok(corpus.length >= 70);
  for (const c of corpus) assertSafe(render(c.markdown), c.name);
});

test("共有の事例集: 許可の入力は、意図した要素が残る", () => {
  const out = (name) => render(corpus.find((c) => c.name === name).markdown);
  assert.match(out("YouTube埋め込み"), /<div data-youtube="ZEguS-om_Ec"><\/div>/);
  assert.match(out("data-videoとdata-carousel"), /<div data-video="featured"><\/div>[\s\S]*<div data-carousel="overview"><\/div>/);
  assert.match(out("details/summary"), /<details>[\s\S]*<summary>見る<\/summary>[\s\S]*<\/details>/);
  assert.match(out("属性なしdivの中にMarkdown（既存のrecall.md形式）"), /<div>[\s\S]*<img src="\/images\/a\.webp" alt="a">[\s\S]*<\/div>/);
  assert.match(out("http/https/mailtoのリンク"), /href="https:\/\/example\.com\/x\?y=1#z"/);
  assert.match(out("サイト内の相対リンクとページ内リンク"), /href="\/news\/foo\/"[\s\S]*href="#section"/);
  assert.match(out("自サイトの画像"), /src="\/images\/imported\/img-1\.webp"[\s\S]*src="\/api\/v1\/public\/media\/AbCdEfGhIjKlMnOpQrStUv"/);
  assert.match(out("通常のMarkdown"), /<h1>見出し<\/h1>[\s\S]*<strong>強調<\/strong>[\s\S]*<blockquote>[\s\S]*<table>/);
  assert.match(out("コードブロック内のHTMLは無害"), /&#x3C;script>alert\(1\)&#x3C;\/script>/); // コードとして表示される（実行されない）
});

// サーバーのパーサー（goldmark）の検査をすり抜け得る、パーサー差異・描画側の攻撃的な入力
const adversarial = {
  "イベント属性つきdiv": '<div data-youtube="ZEguS-om_Ec" onclick="alert(1)"></div>',
  "imgのonerror": "<img src=x onerror=alert(1)>",
  "javascriptのa": '<a href="javascript:alert(1)">x</a>',
  "大文字のJavaScript": '<A HREF="JaVaScRiPt:alert(1)">x</A>',
  "svgのonload": "<svg onload=alert(1)></svg>",
  "mathとmglyph": "<math><mglyph><style><!--</style><img title=\"--&gt;&lt;img src=1 onerror=alert(1)&gt;\">",
  "form": '<form action="https://evil.example"><input name=a></form>',
  "base": '<base href="https://evil.example/">',
  "metaのrefresh": '<meta http-equiv="refresh" content="0;url=https://evil.example">',
  "detailsのontoggle": "<details open ontoggle=alert(1)><summary>x</summary></details>",
  "入れ子のdiv": '<div><div data-youtube="ZEguS-om_Ec" id="x" class="y" style="position:fixed"></div></div>',
  "iframe": '<iframe src="https://www.youtube.com/embed/ZEguS-om_Ec"></iframe>',
  "scriptの分割": "<scr<script>ipt>alert(1)</scr</script>ipt>",
  "コメントに隠す": "<!--><script>alert(1)</script>-->",
  "CDATA": "<![CDATA[<script>alert(1)</script>]]>",
  "NULL文字": "<scr\u0000ipt>alert(1)</scr\u0000ipt>",
  "表のセルのHTML": "| a |\n|---|\n| <img src=x onerror=alert(1)> |",
  "リンクテキストのHTML": "[<img src=x onerror=alert(1)>](https://example.com)",
  "プロトコル相対のリンク": "[x](//evil.example/x)",
  "プロトコル相対の画像": "![x](//evil.example/x.png)",
  "バックスラッシュのURL": "[x](/\\evil.example/x) ![x](/images/..\\x.png)",
  "外部画像": "![x](https://tracker.example/p.png)",
  "dataスキーム": "[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)",
  "エンティティのjavascript": "[x](&#106;avascript:alert(1)) [y](java&#x73;cript:alert(1))",
  "タブ入りjavascriptのa": '<a href="java\tscript:alert(1)">x</a>',
  "autolinkのjavascript": "<javascript:alert(1)>",
  "GFMのwww": "www.evil.example/<script>alert(1)</script>",
  "パス移動の画像": "![x](/images/../../etc/passwd)",
  "参照定義": "[x][r]\n\n[r]: javascript:alert(1)",
  "テンプレート": "<template><script>alert(1)</script></template>",
  "noscript": "<noscript><p title=\"</noscript><img src=x onerror=alert(1)>\"></noscript>",
  "ネストしたテーブルのscript": "<table><tr><td><script>alert(1)</script></td></tr></table>",
  "data属性の値が不正": '<div data-youtube="a" data-video="x" data-carousel="y"></div>',
  "idとname": '<a id="x" name="y" href="https://example.com">x</a>',
};

test("描画側の攻撃的な入力（パーサー差異・生HTML）でも、出力は安全", () => {
  for (const [name, md] of Object.entries(adversarial)) assertSafe(render(md), name);
});

test("YouTubeのdata属性は、11桁のIDだけが残る", () => {
  assert.match(render('<div data-youtube="ZEguS-om_Ec"></div>'), /data-youtube="ZEguS-om_Ec"/);
  for (const bad of ["short", "ZEguS-om_Ec1", "ZEguS om_Ec", 'x" onerror="y']) {
    assert.ok(!render(`<div data-youtube="${bad}"></div>`).includes("data-youtube"), bad);
  }
});

test("プレビューのURL変換: アップロード画像だけを認証つきのプレビュー経路へ読み替える", () => {
  const id = "AbCdEfGhIjKlMnOpQrStUv";
  assert.equal(previewUrlTransform(`/api/v1/public/media/${id}`), `/api/v1/media/by-public/${id}`);
  // 既存記事の画像は公式サイトのファイル。プレビューでは中継経由で表示する
  assert.equal(previewUrlTransform("/images/a.webp"), "/web-images/a.webp");
  assert.equal(previewUrlTransform("/images/contents/2025/x.png"), "/web-images/contents/2025/x.png");
  assert.equal(previewUrlTransform("/images/../etc/passwd"), "/images/../etc/passwd");
  assert.equal(previewUrlTransform("/images/a.webp?x=1"), "/images/a.webp?x=1");
  assert.equal(previewUrlTransform(`/api/v1/public/media/${id}?x=1`), `/api/v1/public/media/${id}?x=1`);
  assert.equal(previewUrlTransform("https://example.com/"), "https://example.com/");
});

test("YouTubeのURLから動画IDを取り出す（許可ホスト・11桁ID）", () => {
  const t = "https://youtu.be/ZEguS-om_Ec と https://www.youtube.com/watch?v=Qo-QI2Z41J8 と https://evil.example/watch?v=aaaaaaaaaaa と https://youtu.be/short";
  assert.deepEqual(youtubeIds(t), ["ZEguS-om_Ec", "Qo-QI2Z41J8"]);
});

test("既存の67記事: 描画の出力は安全で、YouTube・details・画像が保たれる", () => {
  const dir = join(root, "apps/web/content");
  let files = 0, yt = 0;
  for (const sec of readdirSync(dir)) {
    for (const f of readdirSync(join(dir, sec))) {
      let body = readFileSync(join(dir, sec, f), "utf8");
      if (body.startsWith("---\n")) body = body.slice(body.indexOf("\n---\n", 4) + 5);
      body = body.replace(/^<!-- 元URL:[^\n]*-->[ \t]*\n?/gm, "");
      const out = render(body);
      assertSafe(out, `${sec}/${f}`);
      const srcYt = (body.match(/<div data-youtube="[A-Za-z0-9_-]{11}"><\/div>/g) ?? []).length;
      assert.equal((out.match(/<div data-youtube="/g) ?? []).length, srcYt, `${sec}/${f}: YouTubeの埋め込みの目印が保たれる`);
      yt += srcYt;
      assert.equal((out.match(/<details>/g) ?? []).length, (body.match(/<details>/g) ?? []).length, `${sec}/${f}: details`);
      assert.equal((out.match(/<img /g) ?? []).length, (body.match(/!\[[^\]]*\]\(/g) ?? []).length, `${sec}/${f}: 画像の数`);
      files++;
    }
  }
  assert.equal(files, 67);
  assert.ok(yt >= 4, `data-youtubeの埋め込み ${yt}`);
});
