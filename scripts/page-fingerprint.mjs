// ページの「指紋」: 移設前のサイトと、API接続後のサイトが同じ表示かを、機械的に比べるための抽出と比較（docs/design/p3-web.md §4）。
// 本文そのものは保存せず、DOMを「タグ＋許可した属性＋テキスト」に正規化したハッシュと、診断用の構造（カード・見出し・画像・リンク）を取る。
// 使い方: scripts/legacy-fingerprint-capture.mjs（取得・比較のCLI）。smoke（compose-smoke.mjs）からも使う。
import { createHash } from 'node:crypto';

export const sha = s => createHash('sha256').update(s).digest('hex');

/** ブラウザーの中で実行する抽出（自己完結の関数。外の変数を参照しない）。 */
export function extractInPage() {
  const KEEP = new Set(['href', 'src', 'alt', 'role', 'tabindex', 'aria-label', 'datetime', 'class', 'width', 'height', 'loading', 'target', 'rel', 'title', 'type', 'open']);
  const SKIP = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE']);
  const collapse = t => t.replace(/\s+/g, ' ').trim();
  // masked=true のとき、「投稿者：○○」の○○を伏せる（投稿者名は意図した差。公開許可が確認されるまで「nu_digital」になる。D14）
  const norm = (node, masked) => {
    if (node.nodeType === 3) {
      let t = collapse(node.textContent ?? '');
      if (masked && /^投稿者：/.test(t)) t = '投稿者：*';
      return t ? JSON.stringify(t) : '';
    }
    if (node.nodeType !== 1 || SKIP.has(node.tagName)) return '';
    // 「投稿者：○○」の要素は、中身を固定の文字列にする（Reactは「投稿者：」と名前を別のテキストノードにするため）
    if (masked && node.tagName === 'SPAN' && collapse(node.textContent ?? '').startsWith('投稿者：')) return `<span>${JSON.stringify('投稿者：*')}</>`;
    // masked=true: ドットを含むslugの記事URL（例: /blog/nu.metasou/）の末尾スラッシュも揃える（Nextは、ドットを含むパスをファイル扱いにして末尾のスラッシュを付けない。意図した差）
    const val = a => (masked && a.name === 'href' ? a.value.replace(/^(\/(?:blog|news|project)\/[^/]*\.[^/]*)\/$/, '$1') : a.value);
    const attrs = [...node.attributes].filter(a => (KEEP.has(a.name) || a.name.startsWith('data-')) && a.name !== 'style').map(a => `${a.name}=${JSON.stringify(val(a))}`).sort();
    const kids = [...node.childNodes].map(n => norm(n, masked)).filter(Boolean).join('');
    return `<${node.tagName.toLowerCase()}${attrs.length ? ' ' + attrs.join(' ') : ''}>${kids}</>`;
  };
  // 404ページなど<main>が無いページは、body全体を対象にする
  const main = document.querySelector('main') ?? document.body;
  const article = document.querySelector('main article');
  const body = document.querySelector('.article-body');
  const authorEl = main ? [...main.querySelectorAll('span')].find(e => collapse(e.textContent ?? '').startsWith('投稿者：')) : null;
  const authorText = authorEl ? collapse(authorEl.textContent ?? '').replace(/^投稿者：\s*/, '') : null;
  const abs = a => a.getAttribute('href') ?? '';
  const cardRe = /^\/(blog|news|project)\/[^/?#]+\/?$/;
  const cards = main ? [...main.querySelectorAll('a')].filter(a => cardRe.test(abs(a))).map(a => ({
    href: abs(a), text: collapse(a.textContent ?? ''), img: a.querySelector('img')?.getAttribute('src') ?? '',
  })) : [];
  const time = (article ?? main)?.querySelector('time');
  return {
    title: document.title,
    h1: collapse(document.querySelector('h1')?.textContent ?? ''),
    mainDom: main ? norm(main, false) : '',
    articleDom: article ? norm(article, false) : '',
    mainDomMasked: main ? norm(main, true) : '',
    articleDomMasked: article ? norm(article, true) : '',
    author: authorText,
    bodyDom: body ? norm(body, false) : '',
    bodyTextLen: body ? collapse(body.textContent ?? '').length : 0,
    time: time ? { datetime: time.getAttribute('datetime'), text: collapse(time.textContent ?? '') } : null,
    imgs: main ? [...main.querySelectorAll('img')].map(i => ({ src: i.getAttribute('src') ?? '', alt: i.getAttribute('alt') ?? '' })) : [],
    links: main ? [...main.querySelectorAll('a')].map(abs) : [],
    iframes: main ? [...main.querySelectorAll('iframe')].map(f => f.getAttribute('src') ?? '') : [],
    youtubeDataDivs: document.querySelectorAll('[data-youtube]').length,
    headings: main ? [...main.querySelectorAll('h2,h3')].map(h => collapse(h.textContent ?? '')) : [],
    cards,
  };
}

const BOT_UA = 'Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)';

/** 生のHTMLから、メタ情報を取り出す（ストリーミングされるメタデータの位置に依存しないよう、ボットのUAで取る）。 */
export function parseMeta(html) {
  const meta = {};
  const title = html.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
  meta.title = title ? title[1].replace(/\s+/g, ' ').trim() : null;
  for (const m of html.matchAll(/<meta\s+([^>]*?)\/?>/gi)) {
    const attrs = Object.fromEntries([...m[1].matchAll(/([a-zA-Z:-]+)\s*=\s*"([^"]*)"/g)].map(a => [a[1].toLowerCase(), a[2]]));
    const key = attrs.name ?? attrs.property;
    if (key && /^(description|og:.*|twitter:.*)$/.test(key) && attrs.content !== undefined) (meta[key] ??= []).push(attrs.content);
  }
  const canon = html.match(/<link\s+[^>]*rel="canonical"[^>]*href="([^"]*)"/i);
  meta.canonical = canon ? canon[1] : null;
  return meta;
}

/** ページを開いて指紋を取る。pageはPlaywrightのPage、requestはAPIRequestContext。 */
export async function fingerprintUrl(page, request, base, path) {
  const res = await page.goto(base + path, { waitUntil: 'load' });
  const status = res?.status() ?? 0;
  const out = { url: path, status };
  if (status !== 200) return out;
  await page.waitForSelector('main', { timeout: 4000 }).catch(() => {}); // <main>が無いページ（404等）もある
  // 画像の読み込みは待たない（遅延読み込みの画像は画面外では読み込まれず、永久に待つため。指紋は画像の読み込みに依存しない）
  await page.waitForTimeout(400);
  const x = await page.evaluate(extractInPage);
  const raw = await request.get(base + path, { headers: { 'user-agent': BOT_UA }, maxRedirects: 0 });
  const meta = parseMeta(await raw.text());
  return {
    ...out, meta, title: x.title, h1: x.h1, time: x.time, bodyTextLen: x.bodyTextLen, youtubeDataDivs: x.youtubeDataDivs,
    mainDomSha: sha(x.mainDom), articleDomSha: sha(x.articleDom), bodyDomSha: sha(x.bodyDom),
    // 投稿者名を伏せたDOMのハッシュ（意図した差を除いて、DOMが基準と完全一致することを検査する）と、投稿者名
    mainDomShaMasked: sha(x.mainDomMasked), articleDomShaMasked: sha(x.articleDomMasked), author: x.author,
    imgs: x.imgs, links: x.links, iframes: x.iframes, headings: x.headings, cards: x.cards,
  };
}

/** URLの変種（スラッシュなし・index.html・未知のslug・大文字・%2F）のステータスとLocation。リダイレクトは追わない。 */
export async function fingerprintVariants(request, base, articles) {
  const paths = [];
  for (const a of articles) paths.push(`/${a.collection}/${a.slug}`, `/${a.collection}/${a.slug}/index.html`);
  paths.push('/blog/does-not-exist/', '/blog/does-not-exist', '/news/does-not-exist/', '/project/does-not-exist/', '/blog/a%2Fb/', '/nope/', '/blog/', '/blog');
  const out = [];
  for (const p of paths) {
    const r = await request.get(base + p, { maxRedirects: 0 });
    const loc = r.headers()['location'];
    out.push({ url: p, status: r.status(), location: loc ? loc.replace(/^https?:\/\/[^/]+/, '') : null });
  }
  return out;
}

/** 基準と実際を比べて、差を人が読める一覧で返す。knownDiffsは意図した差（`url|項目`）の一覧（無ければ空）。 */
// ドットを含むslugの記事URLの末尾スラッシュの違い（上のnormと同じ。意図した差）は、リンクの比較では揃える
const normHref = h => String(h).replace(/^(\/(?:blog|news|project)\/[^/]*\.[^/]*)\/$/, '$1');
const normLinks = (k, v) => (k === 'links' && Array.isArray(v) ? v.map(normHref) : k === 'cards' && Array.isArray(v) ? v.map(c => ({ ...c, href: normHref(c.href) })) : v);

export function compareFingerprints(baseline, actual, knownDiffs = []) {
  const diffs = [];
  const known = new Set(knownDiffs);
  const push = (url, field, a, b) => { if (!known.has(`${url}|${field}`) && !known.has(`${url}|*`)) diffs.push({ url, field, baseline: a, actual: b }); };
  const byUrl = m => new Map(m.map(p => [p.url, p]));
  const A = byUrl(actual.pages);
  for (const b of baseline.pages) {
    const a = A.get(b.url);
    if (!a) { push(b.url, 'ページが無い', 'あり', 'なし'); continue; }
    for (const k of Object.keys(b)) {
      if (k === 'url') continue;
      const bv = JSON.stringify(normLinks(k, b[k])), av = JSON.stringify(normLinks(k, a[k]));
      if (bv !== av) push(b.url, k, b[k], a[k]);
    }
  }
  for (const a of actual.pages) if (!baseline.pages.some(b => b.url === a.url)) push(a.url, '基準にないページ', 'なし', 'あり');
  const V = byUrl(actual.variants ?? []);
  for (const b of baseline.variants ?? []) {
    const a = V.get(b.url);
    if (!a) { push(b.url, '変種が無い', 'あり', 'なし'); continue; }
    if (a.status !== b.status) push(b.url, 'status', b.status, a.status);
    if ((a.location ?? null) !== (b.location ?? null)) push(b.url, 'location', b.location, a.location);
  }
  return diffs;
}
