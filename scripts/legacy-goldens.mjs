// 既存記事のfrontmatterと日付の「ゴールデン」を、今のサイトと同じパーサー（gray-matter）・同じ関数（content-presentation.mjs）から作る。
// 移行CLI（services/api の import-legacy。YAMLはyaml.v3）の出力を、この正解と照合するために使う（docs/design/p3-web.md §2）。
//   生成: node scripts/legacy-goldens.mjs           → docs/legacy-baseline/frontmatter.json と dates.json
//   検査: node scripts/legacy-goldens.mjs --check   → 記事が変わってゴールデンとずれていないか（CIで実行。ずれたら再生成して差分を確認）
import { createRequire } from 'node:module';
import { readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repo = resolve(fileURLToPath(new URL('..', import.meta.url)));
const web = join(repo, 'apps/web');
const matter = createRequire(join(web, 'x.js'))('gray-matter');
const { formatArticleDate, articleDateOrder } = await import(pathToFileURL(join(web, 'lib/content-presentation.mjs')).href);
const inv = JSON.parse(readFileSync(join(repo, 'docs/legacy-baseline/inventory.json'), 'utf8'));

const KEYS = ['title', 'image', 'author', 'date', 'category', 'excerpt', 'sourceId'];
const frontmatter = [];
const dates = [];
for (const a of [...inv.articles].sort((x, y) => (x.collection + '/' + x.slug).localeCompare(y.collection + '/' + y.slug))) {
  const { data, content } = matter(readFileSync(join(web, a.file), 'utf8'));
  const fm = {}, types = {};
  for (const k of KEYS) {
    const present = Object.prototype.hasOwnProperty.call(data, k);
    // サイトは String(data.x) で読む。型も残して、YAMLの日時型などの取り違えをテストで検出できるようにする
    fm[k] = present ? String(data[k]) : null;
    types[k] = present ? (data[k] instanceof Date ? 'date' : typeof data[k]) : 'absent';
  }
  frontmatter.push({ collection: a.collection, slug: a.slug, fm, types, bodyLength: content.length });
  const label = String(data.date ?? '');
  dates.push({
    collection: a.collection, slug: a.slug, label,
    formatted: formatArticleDate(label), order: articleDateOrder(label),
    // 記事ページの<time dateTime>は、今は「labelがYYYY-MM-DD」のときだけ付く（補った日を開催日として出さない。D12）
    timeDatetime: /^\d{4}-\d{2}-\d{2}$/.test(label) ? label : null,
  });
}
const docs = {
  'docs/legacy-baseline/frontmatter.json': { note: 'gray-matterが読んだfrontmatter（サイトはString()で読む）。再生成: node scripts/legacy-goldens.mjs', articles: frontmatter },
  'docs/legacy-baseline/dates.json': { note: 'formatArticleDate・articleDateOrderの出力。再生成: node scripts/legacy-goldens.mjs', articles: dates },
};
let bad = 0;
for (const [file, doc] of Object.entries(docs)) {
  const text = JSON.stringify(doc, null, 1) + '\n';
  const path = join(repo, file);
  if (process.argv.includes('--check')) {
    let cur = '';
    try { cur = readFileSync(path, 'utf8'); } catch { /* 無い */ }
    if (cur !== text) { console.error(`FAIL: ${file} が記事の現状と一致しません（node scripts/legacy-goldens.mjs で再生成して差分を確認）`); bad++; }
  } else {
    writeFileSync(path, text);
    console.log(`書き出しました: ${file}（${doc.articles.length}件）`);
  }
}
if (process.argv.includes('--check')) {
  if (bad) process.exit(1);
  console.log(`PASS: frontmatter・日付のゴールデンが記事${inv.articles.length}件の現状と一致`);
}
