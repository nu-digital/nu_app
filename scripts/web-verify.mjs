// 稼働中の公式サイト（公開APIから描画する動的配信）が、移設前基準(docs/legacy-baseline/inventory.json)と一致するかを検証する。ブラウザー不要。
// 使い方: node scripts/web-verify.mjs <サイトのURL（例: http://127.0.0.1:3100）> [apps/web]
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(fileURLToPath(new URL('..', import.meta.url)));
const base = String(process.argv[2] ?? '').replace(/\/$/, '');
if (!/^https?:\/\//.test(base)) { console.error('使い方: node scripts/web-verify.mjs <サイトのURL> [apps/web]'); process.exit(2); }
const web = resolve(process.argv[3] ?? join(repo, 'apps/web'));
const inv = JSON.parse(readFileSync(join(repo, 'docs/legacy-baseline/inventory.json'), 'utf8'));
const sha = path => createHash('sha256').update(readFileSync(path)).digest('hex');
const walk = d => readdirSync(d, { withFileTypes: true }).flatMap(e => e.isDirectory() ? walk(join(d, e.name)) : [join(d, e.name)]);
const takedown = JSON.parse(readFileSync(join(repo, 'docs/legacy-baseline/takedown.json'), 'utf8'));
const tdArticles = new Set(takedown.articles.map(a => `${a.section}/${a.slug}`));
const tdImages = new Set(takedown.images.map(i => i.path));
const errors = [];
const articleKey = a => `${a.collection}/${a.slug}`;
const NOT_PAGES = new Set(['/404/', '/_not-found/']); // 静的exportの実在ページではなくなった（known-diffs.jsonに理由）

// 1. ルート（末尾スラッシュ付き。大文字・ドット入りslugを含む）: 200でh1がある。取り下げた記事は404
let ok = 0;
for (const r of inv.routes) {
  if (NOT_PAGES.has(r)) continue;
  const a = inv.articles.find(x => x.url === r);
  const res = await fetch(base + r, { redirect: 'follow' });
  const text = await res.text();
  if (a && tdArticles.has(articleKey(a))) { if (res.status !== 404) errors.push(`取り下げた記事が公開されています: ${r} (${res.status})`); continue; }
  if (res.status !== 200) { errors.push(`ルートが200でない: ${r} (${res.status})`); continue; }
  if (r !== '/' && !text.includes('<h1')) errors.push(`h1なし: ${r}`);
  ok++;
}
// 未知のURLは404
for (const r of ['/blog/does-not-exist/', '/news/does-not-exist/', '/project/does-not-exist/', '/no-such-page/']) {
  const res = await fetch(base + r, { redirect: 'follow' });
  if (res.status !== 404) errors.push(`未知のURLが404でない: ${r} (${res.status})`);
}

// 2. 記事: ファイル(frontmatter+本文)のsha256（移行の入力が変わっていない）
for (const a of inv.articles) {
  if (tdArticles.has(articleKey(a))) continue; // 取り下げ済み（ファイルは無くてよい）
  const file = join(web, a.file);
  if (!existsSync(file)) { errors.push(`記事欠落: ${a.file}`); continue; }
  if (sha(file) !== a.fileSha256) errors.push(`記事が変更されています: ${a.file}`);
}

// 3. 画像: public/images の全ファイルがsha256まで一致し、サイトからも同じバイト列で配信される
for (const img of inv.publicImages) {
  const src = join(web, 'public', img.path);
  if (tdImages.has(img.path)) { // 取り下げ済み: 存在しないことを検査する（残っていれば失敗）
    if (existsSync(src)) errors.push(`取り下げ済みの画像が残っています: ${img.path}`);
    const res = await fetch(base + img.path);
    if (res.status !== 404) errors.push(`取り下げ済みの画像が配信されています: ${img.path} (${res.status})`);
    continue;
  }
  if (!existsSync(src)) { errors.push(`画像欠落: ${img.path}`); continue; }
  if (sha(src) !== img.sha256) errors.push(`画像が変更されています: ${img.path}`);
  const res = await fetch(base + img.path);
  const bytes = Buffer.from(await res.arrayBuffer());
  if (res.status !== 200 || createHash('sha256').update(bytes).digest('hex') !== img.sha256) errors.push(`配信される画像が不一致/なし: ${img.path} (${res.status})`);
}
const extra = walk(join(web, 'public', 'images')).length - (inv.publicImages.length - tdImages.size);
if (extra !== 0) errors.push(`public/images のファイル数が基準と異なります(差 ${extra})`);

if (errors.length) { console.error(errors.slice(0, 30).join('\n')); console.error(`FAIL: ${errors.length}件`); process.exit(1); }
console.log(`PASS: ルート${ok}、記事${inv.articles.length - tdArticles.size}、画像${inv.publicImages.length - tdImages.size}が移設前基準と一致（取り下げ: 記事${tdArticles.size}・画像${tdImages.size}）`);
