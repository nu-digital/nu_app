// 稼働中のサイトから全URLの「指紋」を取る（docs/design/p3-web.md §4）。
//   取得: node scripts/legacy-fingerprint-capture.mjs --base http://127.0.0.1:8080 --out docs/legacy-baseline/pages.json [--twice]
//   比較: node scripts/legacy-fingerprint-capture.mjs --base http://127.0.0.1:8080 --compare docs/legacy-baseline/pages.json [--known known-diffs.json]
// Playwrightは PLAYWRIGHT_DIR（既定 apps/web）のものを使う。--twice は2回取って一致を確かめる（フレークの検出）。
import { createRequire } from 'node:module';
import { readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compareFingerprints, fingerprintUrl, fingerprintVariants } from './page-fingerprint.mjs';

const repo = resolve(fileURLToPath(new URL('..', import.meta.url)));
const arg = n => { const i = process.argv.indexOf(`--${n}`); return i < 0 ? null : (process.argv[i + 1]?.startsWith('--') ? true : process.argv[i + 1] ?? true); };
const base = String(arg('base') ?? '').replace(/\/$/, '');
if (!base) { console.error('--base が必要です'); process.exit(2); }
const require = createRequire(join(resolve(process.env.PLAYWRIGHT_DIR ?? join(repo, 'apps/web')), 'x.js'));
const { chromium } = require('playwright');
const inv = JSON.parse(readFileSync(join(repo, 'docs/legacy-baseline/inventory.json'), 'utf8'));

export async function capture(base) {
  const browser = await chromium.launch();
  try {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'ja-JP', timezoneId: 'Asia/Tokyo', reducedMotion: 'reduce' });
    const page = await ctx.newPage();
    const pages = [];
    for (const r of [...inv.routes].sort()) pages.push(await fingerprintUrl(page, ctx.request, base, r));
    const arts = inv.articles;
    // 区分ごとの代表と、大文字・ドットを含むslug（URLの変種のステータスとLocationを見る）
    const special = arts.filter(a => /[A-Z.]/.test(a.slug)).slice(0, 2);
    const reps = ['blog', 'news', 'project'].map(c => arts.find(a => a.collection === c)).filter(Boolean);
    const variants = await fingerprintVariants(ctx.request, base, [...new Map([...reps, ...special].map(a => [a.collection + '/' + a.slug, a])).values()]);
    return { pages, variants };
  } finally {
    await browser.close();
  }
}

const result = await capture(base);
const bad = result.pages.filter(p => p.status !== 200);
if (bad.length) console.error(`警告: 200でないページ ${bad.length}件: ${bad.slice(0, 5).map(p => `${p.url}=${p.status}`).join(', ')}`);
if (arg('twice')) {
  const again = await capture(base);
  const diffs = compareFingerprints(result, again);
  if (diffs.length) { console.error(`FAIL: 2回の取得が一致しません（フレーク）: ${diffs.length}件`); for (const d of diffs.slice(0, 10)) console.error(`  ${d.url} ${d.field}`); process.exit(1); }
  console.log(`2回の取得が一致しました（${result.pages.length}ページ・${result.variants.length}変種）`);
}
if (arg('out')) {
  const doc = { note: '移設前の公開サイトの「指紋」（docs/design/p3-web.md §4）。本文そのものは含まない。再生成: docs/legacy-baseline/README.md', pages: result.pages, variants: result.variants };
  writeFileSync(resolve(String(arg('out'))), JSON.stringify(doc, null, 1) + '\n');
  console.log(`書き出しました: ${arg('out')}（ページ${result.pages.length}・変種${result.variants.length}）`);
}
if (arg('compare')) {
  const baseline = JSON.parse(readFileSync(resolve(String(arg('compare'))), 'utf8'));
  const knownRaw = arg('known') && arg('known') !== true ? JSON.parse(readFileSync(resolve(String(arg('known'))), 'utf8')) : [];
  const known = knownRaw.map(k => (typeof k === 'string' ? k : `${k.url}|${k.field}`)); // 意図した差は {url, field, reason} で書く
  const diffs = compareFingerprints(baseline, result, known);
  if (arg('dump')) writeFileSync(resolve(String(arg('dump'))), JSON.stringify(diffs, null, 1));
  if (diffs.length) {
    // 並びの食い違いを見つけやすいよう、カードはhrefの列で示す（先頭から最初に違う位置を含む）
    const show = (f, v) => (f === 'cards' && Array.isArray(v) ? v.map(c => c.href) : v);
    for (const d of diffs.slice(0, 40)) {
      const b = JSON.stringify(show(d.field, d.baseline)) ?? '', a = JSON.stringify(show(d.field, d.actual)) ?? '';
      let i = 0; while (i < b.length && b[i] === a[i]) i++;
      const w = d.field === 'cards' ? 120 : 80;
      console.error(`差: ${d.url} [${d.field}] 基準=${b.slice(0, w)} 実際=${a.slice(0, w)}` + (i < Math.max(a.length, b.length) && d.field !== 'mainDomSha' && !/Sha/.test(d.field) ? ` ／最初の違い(位置${i}): 基準=…${b.slice(Math.max(0, i - 20), i + 60)} 実際=…${a.slice(Math.max(0, i - 20), i + 60)}` : ''));
    }
    console.error(`FAIL: 基準との差 ${diffs.length}件`);
    process.exit(1);
  }
  console.log(`PASS: ${result.pages.length}ページ・${result.variants.length}変種が基準と一致`);
}
