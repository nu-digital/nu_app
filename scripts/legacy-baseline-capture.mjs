// 既存サイトの静的build成果物(out)を配信し、全ルートHTTP確認・基準画面撮影・ギャラリー操作確認を行う。
// 使い方: PLAYWRIGHT_DIR=<playwrightのあるディレクトリ(例: apps/web)> node scripts/legacy-baseline-capture.mjs <out dir> <出力dir> [basePath]
import { createServer } from 'node:http';
import { readFileSync, existsSync, statSync, mkdirSync, writeFileSync } from 'node:fs';
import { join, resolve, extname } from 'node:path';
import { createRequire } from 'node:module';

const [outArg, shotArg, basePath = ''] = process.argv.slice(2);
const out = resolve(outArg), shots = resolve(shotArg);
mkdirSync(shots, { recursive: true });
const { chromium } = createRequire(join(resolve(process.env.PLAYWRIGHT_DIR), 'x.js'))('playwright');
const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.webp': 'image/webp', '.png': 'image/png', '.jpg': 'image/jpeg', '.svg': 'image/svg+xml', '.json': 'application/json', '.woff2': 'font/woff2', '.txt': 'text/plain' };

const server = createServer((req, res) => {
  let p = decodeURIComponent(new URL(req.url, 'http://x').pathname);
  if (basePath) { if (!p.startsWith(basePath + '/') && p !== basePath) { res.writeHead(404).end(); return; } p = p.slice(basePath.length) || '/'; }
  let f = join(out, p);
  if (existsSync(f) && statSync(f).isDirectory()) f = join(f, 'index.html');
  if (!existsSync(f)) { f = join(out, '404.html'); res.statusCode = 404; }
  res.setHeader('content-type', types[extname(f)] || 'application/octet-stream');
  res.end(readFileSync(f));
}).listen(0, '127.0.0.1');
await new Promise(r => server.once('listening', r));
const base = `http://127.0.0.1:${server.address().port}${basePath}`;

const inv = JSON.parse(readFileSync(new URL('../docs/legacy-baseline/inventory.json', import.meta.url)));
const result = { base: basePath || '(root)', routes: [], gallery: [], viewports: [], notes: [] };
const browser = await chromium.launch();

// networkidle はNext.jsのリンクprefetch等で通信が止まらずCIで不安定なため使わない(Playwrightも非推奨)。
// ページの load 後に、ビューポート内の画像の読込完了とWebフォントの準備を待つ。
async function open(page, url) {
  const resp = await page.goto(url, { waitUntil: 'load', timeout: 60000 });
  await page.evaluate(async () => {
    await document.fonts.ready;
    const inView = [...document.images].filter(i => { const r = i.getBoundingClientRect(); return r.bottom > 0 && r.top < innerHeight; });
    await Promise.all(inView.map(i => i.complete ? null : new Promise(res => { i.addEventListener('load', res, { once: true }); i.addEventListener('error', res, { once: true }); })));
  });
  return resp;
}
const log = [];

// 1. 全ルートのHTTPステータスと、画像・ネットワーク失敗の検出
{
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  let failures = [];
  // Next.jsのリンクprefetchは遷移時にERR_ABORTEDで中断されるため、同一オリジンの中断は失敗に数えない。外部(YouTube等)は別集計。
  const externalFailures = new Set();
  page.on('requestfailed', r => {
    const local = r.url().startsWith(base);
    if (!local) externalFailures.add(new URL(r.url()).host);
    else if (!/ERR_ABORTED/.test(r.failure()?.errorText ?? '')) failures.push(`requestfailed ${r.url()}`);
  });
  page.on('response', r => { if (r.status() >= 400 && r.url().startsWith(base) && !r.url().endsWith('/favicon.ico')) failures.push(`${r.status()} ${r.url()}`); });
  const consoleErrors = [];
  page.on('console', m => { if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 200)); });
  for (const route of inv.routes.filter(r => !['/404/', '/_not-found/'].includes(r))) {
    failures = []; consoleErrors.length = 0;
    const resp = await open(page, base + route);
    const broken = await page.$$eval('img', imgs => imgs.filter(i => i.complete && i.naturalWidth === 0 && i.currentSrc).map(i => i.currentSrc));
    result.routes.push({ route, status: resp.status(), h1: (await page.textContent('h1').catch(() => null))?.trim().slice(0, 60) ?? null, brokenImages: broken.length, failures: failures.length, failureUrls: failures.slice(0, 10), consoleErrors: [...consoleErrors] });
  }
  result.externalFailedHosts = [...externalFailures];
  const nf = await page.goto(base + '/no-such-page/');
  result.notFoundStatus = nf.status();
  await ctx.close();
}

// 2. 基準画面
const shotRoutes = [['home', '/'], ['blog', '/blog/'], ['news', '/news/'], ['project', '/project/'], ['terms', '/terms/'], ['blog-gallery-surugadai', '/blog/surugadai-ourisai-2026/'], ['blog-dot-slug-nu.metasou', '/blog/nu.metasou/'], ['project-nu-bar', '/project/nu-bar/']];
const viewports = [['pc', 1440, 900], ['sp', 390, 844]];
for (const [vn, width, height] of viewports) {
  const ctx = await browser.newContext({ viewport: { width, height }, reducedMotion: 'reduce', deviceScaleFactor: 1, locale: 'ja-JP', timezoneId: 'Asia/Tokyo' });
  const page = await ctx.newPage();
  for (const [name, route] of shotRoutes) {
    await open(page, base + route);
    await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(300);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    const file = `${vn}-${name}.png`;
    await page.screenshot({ path: join(shots, file), fullPage: false });
    result.viewports.push({ file, viewport: `${width}x${height}`, horizontalOverflowPx: overflow });
  }
  await ctx.close();
}

// 3. ギャラリー操作
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
const page = await ctx.newPage();
await open(page, base + '/blog/surugadai-ourisai-2026/');
const imgs = page.locator('.article-gallery img');
const total = await page.locator('article img, main img').count();
const check = (name, ok, detail = '') => result.gallery.push({ name, ok: !!ok, detail });
const dialogOpen = () => page.evaluate(() => !!document.querySelector('dialog.photo-lightbox[open]'));
const counter = () => page.locator('.photo-lightbox-toolbar span').textContent();
const focusedIsOpener = idx => page.evaluate(i => document.activeElement === document.querySelectorAll('.article-gallery img')[i], idx);

check('ギャラリー画像数>0', (await imgs.count()) > 0, `.article-gallery img = ${await imgs.count()}`);
for (const [label, act] of [['クリック', el => el.click()], ['Enter', async el => { await el.focus(); await page.keyboard.press('Enter'); }], ['Space', async el => { await el.focus(); await page.keyboard.press('Space'); }]]) {
  const el = imgs.nth(2);
  await el.scrollIntoViewIfNeeded();
  await act(el);
  check(`${label}で開く`, await dialogOpen(), await counter().catch(() => ''));
  check(`${label}: 開いた時の背景スクロール抑止`, await page.evaluate(() => document.body.style.overflow === 'hidden'));
  check(`${label}: 閉じるボタンにフォーカス`, await page.evaluate(() => document.activeElement?.getAttribute('aria-label') === '拡大表示を閉じる'));
  if (label === 'クリック') {
    const c0 = await counter();
    await page.keyboard.press('ArrowRight'); const c1 = await counter();
    await page.keyboard.press('ArrowLeft'); const c2 = await counter();
    check('→/←で移動', c0 !== c1 && c0 === c2, `${c0} → ${c1} → ${c2}`);
    await page.getByRole('button', { name: '次の写真' }).click(); const c3 = await counter();
    check('次へボタン', c3 !== c0, c3);
    await page.getByRole('button', { name: '前の写真' }).click();
    check('前へボタン', (await counter()) === c0);
  }
  await page.keyboard.press('Escape');
  check(`${label}: Escで閉じる`, !(await dialogOpen()));
  check(`${label}: 開いた画像へフォーカス復帰`, await focusedIsOpener(2));
  check(`${label}: 背景スクロール復帰`, await page.evaluate(() => document.body.style.overflow !== 'hidden'));
}
await imgs.nth(1).click(); await page.getByRole('button', { name: '拡大表示を閉じる' }).click();
check('閉じるボタンで閉じ、フォーカス復帰', !(await dialogOpen()) && await focusedIsOpener(1));
await imgs.nth(0).click(); await page.mouse.click(5, 5);
check('背景クリックで閉じる', !(await dialogOpen()));
await imgs.nth(0).click();
for (let i = 0; i < 40 && !(await counter()).startsWith('1 /'); i++) await page.keyboard.press('ArrowLeft');
const first = await counter();
await page.keyboard.press('ArrowLeft'); const wrap = await counter();
const n = Number(first.split('/')[1]);
check('先頭(1枚目)から←で末尾へ循環', first.startsWith('1 /') && wrap.startsWith(`${n} /`), `${first} → ${wrap}`);
await page.keyboard.press('Escape');
await imgs.nth(0).click(); await page.screenshot({ path: join(shots, 'pc-gallery-lightbox-open.png') });
await ctx.close();
await browser.close(); server.close();
writeFileSync(join(shots, 'capture-result.json'), JSON.stringify(result, null, 2));
const bad = result.routes.filter(r => r.status !== 200 || r.brokenImages || r.failures);
console.log(JSON.stringify({ base: result.base, routes: result.routes.length, nonOk: bad.length, notFoundStatus: result.notFoundStatus, galleryFail: result.gallery.filter(g => !g.ok).map(g => g.name), galleryChecks: result.gallery.length }, null, 1));
if (bad.length) console.log(JSON.stringify(bad, null, 1).slice(0, 3000));
// CIで使えるよう、失敗(非200・壊れた画像・ギャラリー不合格・notFoundが404でない・横溢れ)は終了コード1にする。
const overflow = result.viewports.filter(v => v.horizontalOverflowPx > 0);
if (overflow.length) console.log('horizontal overflow:', JSON.stringify(overflow));
if (bad.length || result.gallery.some(g => !g.ok) || result.notFoundStatus !== 404 || overflow.length) process.exitCode = 1;
