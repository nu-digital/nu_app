// 既存サイト(nu_digital)の記事・画像・ルートの棚卸し。Node標準のみ。
// 使い方: node scripts/legacy-inventory.mjs <legacy-site dir> [<out-root dir>]  (JSONを標準出力)
import { readFileSync, readdirSync, existsSync, statSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, resolve } from 'node:path';

const [legacyArg, outArg] = process.argv.slice(2);
if (!legacyArg) { console.error('usage: legacy-inventory.mjs <legacy dir> [out dir]'); process.exit(2); }
const legacy = resolve(legacyArg);
const sha = buf => createHash('sha256').update(buf).digest('hex');
const unq = v => v.replace(/^(['"])(.*)\1$/, '$2');

function frontmatter(text) {
  const m = text.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?/);
  if (!m) return { data: {}, body: text, hasFrontmatter: false };
  const data = {};
  for (const line of m[1].split(/\r?\n/)) {
    const kv = line.match(/^([A-Za-z_]\w*):\s*(.*)$/);
    if (kv) data[kv[1]] = unq(kv[2].trim());
  }
  return { data, body: text.slice(m[0].length), hasFrontmatter: true };
}

// 本文中の画像参照(Markdown/HTML img)と内部リンクを抽出
const imageRefs = body => [...new Set([
  ...[...body.matchAll(/!\[[^\]]*\]\(([^)\s]+)/g)].map(m => m[1]),
  ...[...body.matchAll(/<img[^>]+src=["']([^"']+)["']/g)].map(m => m[1]),
])];
const internalLinks = body => [...new Set([...body.matchAll(/\]\((\/[^)\s]*)\)/g)].map(m => m[1]))];

const imgCache = new Map();
function imageInfo(ref) {
  if (!ref.startsWith('/')) return { ref, external: true };
  if (!imgCache.has(ref)) {
    const file = join(legacy, 'public', decodeURIComponent(ref));
    imgCache.set(ref, existsSync(file)
      ? { ref, bytes: statSync(file).size, sha256: sha(readFileSync(file)) }
      : { ref, missing: true });
  }
  return imgCache.get(ref);
}

const articles = [];
for (const collection of ['blog', 'news', 'project']) {
  const dir = join(legacy, 'content', collection);
  for (const name of readdirSync(dir).filter(n => n.endsWith('.md')).sort()) {
    const raw = readFileSync(join(dir, name));
    const { data, body, hasFrontmatter } = frontmatter(raw.toString('utf8'));
    const slug = name.slice(0, -3);
    const refs = imageRefs(body);
    const cover = data.image ? imageInfo(data.image) : null;
    articles.push({
      collection, slug, url: `/${collection}/${slug}/`, file: `content/${collection}/${name}`,
      fileSha256: sha(raw), bodySha256: sha(body), hasFrontmatter,
      title: data.title ?? null, date: data.date ?? null, category: data.category ?? null,
      author: data.author ?? null, sourceId: data.sourceId ?? null, excerptLength: (data.excerpt ?? '').length,
      cover, bodyImages: refs.map(imageInfo), internalLinks: internalLinks(body),
      embeds: [...body.matchAll(/<(iframe|video|script)\b/gi)].length,
    });
  }
}

const imgDir = join(legacy, 'public', 'images');
const walk = d => readdirSync(d, { withFileTypes: true }).flatMap(e =>
  e.isDirectory() ? walk(join(d, e.name)) : [join(d, e.name)]);
const publicImages = walk(imgDir).sort().map(f => ({
  path: f.slice(join(legacy, 'public').length), bytes: statSync(f).size, sha256: sha(readFileSync(f)),
}));
const used = new Set(articles.flatMap(a => [a.cover?.ref, ...a.bodyImages.map(i => i.ref)].filter(Boolean)));

const routes = [];
if (outArg) {
  const out = resolve(outArg);
  for (const f of walk(out).sort()) if (f.endsWith('index.html')) routes.push(f.slice(out.length).replace(/index\.html$/, '') || '/');
}

const count = k => Object.fromEntries(Object.entries(Object.groupBy(articles, a => a[k] ?? '(none)')).map(([a, b]) => [a, b.length]));
console.log(JSON.stringify({
  summary: {
    articles: articles.length, byCollection: count('collection'), byCategory: count('category'),
    publicImageFiles: publicImages.length, publicImageBytes: publicImages.reduce((s, i) => s + i.bytes, 0),
    referencedImages: used.size, unreferencedImages: publicImages.filter(i => !used.has(i.path)).length,
    missingImages: articles.flatMap(a => [a.cover, ...a.bodyImages].filter(i => i?.missing).map(i => i.ref)),
    slugsWithUppercaseOrDot: articles.filter(a => /[A-Z.]/.test(a.slug)).map(a => a.url),
    nonIsoDates: articles.filter(a => !/^\d{4}-\d{2}-\d{2}$/.test(a.date ?? '')).map(a => ({ url: a.url, date: a.date })),
    categoryMismatch: articles.filter(a => (a.collection === 'project') !== (a.category === 'Product')).map(a => ({ url: a.url, category: a.category })),
    routeCount: routes.length,
  },
  routes, articles, publicImages,
}, null, 2));
