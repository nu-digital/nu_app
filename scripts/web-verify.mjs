// apps/web のbuild出力が、移設前基準(docs/legacy-baseline/inventory.json)と一致するかを検証する。ブラウザー不要。
// 使い方: node scripts/web-verify.mjs [apps/web] [apps/web/out]
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(fileURLToPath(new URL('..', import.meta.url)));
const web = resolve(process.argv[2] ?? join(repo, 'apps/web'));
const out = resolve(process.argv[3] ?? join(web, 'out'));
const inv = JSON.parse(readFileSync(join(repo, 'docs/legacy-baseline/inventory.json'), 'utf8'));
const sha = path => createHash('sha256').update(readFileSync(path)).digest('hex');
const walk = d => readdirSync(d, { withFileTypes: true }).flatMap(e => e.isDirectory() ? walk(join(d, e.name)) : [join(d, e.name)]);
const takedown = JSON.parse(readFileSync(join(repo, 'docs/legacy-baseline/takedown.json'), 'utf8'));
const tdArticles = new Set(takedown.articles.map(a => `${a.section}/${a.slug}`));
const tdImages = new Set(takedown.images.map(i => i.path));
const errors = [];

if (!existsSync(out)) { console.error(`FAIL: build出力がありません: ${out}`); process.exit(1); }

// 1. ルート（末尾スラッシュ付き。大文字・ドット入りslugを含む）
const articleKey = a => `${a.collection}/${a.slug}`;
const routes = walk(out).filter(f => f.endsWith('index.html')).map(f => f.slice(out.length).replace(/index\.html$/, '') || '/').sort();
const expected = inv.routes.filter(r => !inv.articles.some(a => a.url === r && tdArticles.has(articleKey(a)))).sort(); // 取り下げた記事のルートは無いのが正
for (const r of expected) if (!routes.includes(r)) errors.push(`ルート欠落: ${r}`);
for (const r of routes) if (!expected.includes(r)) errors.push(`基準にないルート: ${r}`);

// 2. 記事: ファイル(frontmatter+本文)のsha256と、出力HTMLの存在・h1
for (const a of inv.articles) {
  if (tdArticles.has(articleKey(a))) continue; // 取り下げ済み（ファイル・ルートは無くてよい）
  const file = join(web, a.file);
  if (!existsSync(file)) { errors.push(`記事欠落: ${a.file}`); continue; }
  if (sha(file) !== a.fileSha256) errors.push(`記事が変更されています: ${a.file}`);
  const html = join(out, a.url, 'index.html');
  if (!existsSync(html)) errors.push(`記事HTMLなし: ${a.url}`);
  else if (!readFileSync(html, 'utf8').includes('<h1')) errors.push(`記事HTMLにh1なし: ${a.url}`);
}

// 3. 画像: public/images の全ファイルがsha256まで一致し、出力へ複製されている
for (const img of inv.publicImages) {
  const src = join(web, 'public', img.path), dst = join(out, img.path);
  if (tdImages.has(img.path)) { // 取り下げ済み: 存在しないことを検査する（残っていれば失敗）
    if (existsSync(src)) errors.push(`取り下げ済みの画像が残っています: ${img.path}`);
    if (existsSync(dst)) errors.push(`取り下げ済みの画像が出力に残っています: ${img.path}`);
    continue;
  }
  if (!existsSync(src)) { errors.push(`画像欠落: ${img.path}`); continue; }
  if (sha(src) !== img.sha256) errors.push(`画像が変更されています: ${img.path}`);
  if (!existsSync(dst) || sha(dst) !== img.sha256) errors.push(`出力画像が不一致/なし: ${img.path}`);
}
const extra = walk(join(web, 'public', 'images')).length - (inv.publicImages.length - tdImages.size);
if (extra !== 0) errors.push(`public/images のファイル数が基準と異なります(差 ${extra})`);

if (errors.length) { console.error(errors.slice(0, 30).join('\n')); console.error(`FAIL: ${errors.length}件`); process.exit(1); }
console.log(`PASS: ルート${routes.length}、記事${inv.articles.length - tdArticles.size}、画像${inv.publicImages.length - tdImages.size}が移設前基準と一致（取り下げ: 記事${tdArticles.size}・画像${tdImages.size}）`);
