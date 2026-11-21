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
  const norm = node => {
    if (node.nodeType === 3) { const t = collapse(node.textContent ?? ''); return t ? JSON.stringify(t) : ''; }
    if (node.nodeType !== 1 || SKIP.has(node.tagName)) return '';
    const attrs = [...node.attributes].filter(a => (KEEP.has(a.name) || a.name.startsWith('data-')) && a.name !== 'style').map(a => `${a.name}=${JSON.stringify(a.value)}`).sort();
    const kids = [...node.childNodes].map(norm).filter(Boolean).join('');
    return `<${node.tagName.toLowerCase()}${attrs.length ? ' ' + attrs.join(' ') : ''}>${kids}</>`;
  };
  // 404ページなど<main>が無いページは、body全体を対象にする
  const main = document.querySelector('main') ?? document.body;
  const article = document.querySelector('main article');
  const body = document.querySelector('.article-body');
  const abs = a => a.getAttribute('href') ?? '';
  const cardRe = /^\/(blog|news|project)\/[^/?#]+\/?$/;
  const cards = main ? [...main.querySelectorAll('a')].filter(a => cardRe.test(abs(a))).map(a => ({
    href: abs(a), text: collapse(a.textContent ?? ''), img: a.querySelector('img')?.getAttribute('src') ?? '',
  })) : [];
  const time = (article ?? main)?.querySelector('time');
  return {
    title: document.title,
    h1: collapse(document.querySelector('h1')?.textContent ?? ''),
    mainDom: main ? norm(main) : '',
    articleDom: article ? norm(article) : '',
    bodyDom: body ? norm(body) : '',
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
export function compareFingerprints(baseline, actual, knownDiffs = []) {
  const diffs = [];
  const known = new Set(knownDiffs);
  const push = (url, field, a, b) => { if (!known.has(`${url}|${field}`)) diffs.push({ url, field, baseline: a, actual: b }); };
  const byUrl = m => new Map(m.map(p => [p.url, p]));
  const A = byUrl(actual.pages);
  for (const b of baseline.pages) {
    const a = A.get(b.url);
    if (!a) { push(b.url, 'ページが無い', 'あり', 'なし'); continue; }
    for (const k of Object.keys(b)) {
      if (k === 'url') continue;
      const bv = JSON.stringify(b[k]), av = JSON.stringify(a[k]);
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
