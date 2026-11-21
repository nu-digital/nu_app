// 公開API（services/api）の最小の擬似サーバー。webのCI・ローカル確認用（APIとDBが無くても全ルートを描画して検査できる）。
//   node scripts/mock-public-api.mjs [--port 8080] [--content apps/web/content]
// `content/*.md` を gray-matter で読み、移行（nuctl import-legacy）と同じ規則で /api/v1/public/contents* を返す。
// 投稿者は、移行直後の状態（公開許可が未確認）と同じく、すべて「nu_digital」。実APIとの差は compose smoke の比較で検出する。
import { createRequire } from 'node:module';
import { readdirSync, readFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repo = resolve(fileURLToPath(new URL('..', import.meta.url)));
const arg = (n, d) => { const i = process.argv.indexOf(`--${n}`); return i < 0 ? d : process.argv[i + 1]; };
const port = Number(arg('port', process.env.MOCK_API_PORT ?? 8080));
const contentDir = resolve(arg('content', join(repo, 'apps/web/content')));
const require = createRequire(join(repo, 'apps/web', 'x.js'));
const matter = require('gray-matter');
const { articleDateOrder } = await import(pathToFileURL(join(repo, 'apps/web/lib/content-presentation.mjs')).href);

function load() {
  const items = [];
  for (const section of ['blog', 'news', 'project']) {
    for (const f of readdirSync(join(contentDir, section)).filter(n => n.endsWith('.md')).sort()) {
      const { data, content } = matter(readFileSync(join(contentDir, section, f), 'utf8'));
      const body = content.replace(/^<!-- 元URL:[^\n]*-->[ \t]*\n?/gm, '');
      const label = String(data.date ?? '');
      const ms = articleDateOrder(label);
      const metadata = { category: String(data.category ?? ''), image: String(data.image ?? '') };
      if (data.excerpt) metadata.excerpt = String(data.excerpt);
      items.push({
        type: section === 'project' ? 'project' : 'article', section, slug: f.replace(/\.md$/, ''),
        title: String(data.title ?? '').trim(), summary: String(data.excerpt ?? ''), author: 'nu_digital', metadata,
        date: { label, kind: 'exact', sortDate: ms > 0 ? new Date(ms).toISOString().slice(0, 10) : null, sortDateIsEstimated: false },
        markdown: body, contributors: [],
      });
    }
  }
  return items;
}
const all = load();
const strip = ({ markdown, contributors, ...item }) => item; // eslint-disable-line no-unused-vars
const order = [...all].sort((a, b) => (b.date.sortDate ?? '').localeCompare(a.date.sortDate ?? '') || (a.slug < b.slug ? -1 : 1));

const send = (res, status, body, extra = {}) => {
  res.writeHead(status, { 'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-cache', ...extra });
  res.end(JSON.stringify(body) + '\n');
};
createServer((req, res) => {
  const url = new URL(req.url, 'http://x');
  if (req.method !== 'GET' && req.method !== 'HEAD') return send(res, 405, { error: { code: 'METHOD_NOT_ALLOWED' } });
  if (url.pathname === '/healthz') return send(res, 200, { status: 'ok' });
  if (url.pathname === '/api/v1/public/contents') {
    const section = url.searchParams.get('section');
    const limit = Math.min(100, Math.max(1, Number(url.searchParams.get('limit') ?? 50)));
    const pool = section ? order.filter(i => i.section === section) : order;
    const start = Number(url.searchParams.get('cursor') ?? 0) || 0;
    const page = pool.slice(start, start + limit);
    return send(res, 200, { items: page.map(strip), nextCursor: start + limit < pool.length ? String(start + limit) : null });
  }
  const m = /^\/api\/v1\/public\/contents\/(blog|news|project)\/([^/]+)$/.exec(url.pathname);
  if (m) {
    const item = all.find(i => i.section === m[1] && i.slug === decodeURIComponent(m[2]));
    return item ? send(res, 200, item) : send(res, 404, { error: { code: 'NOT_FOUND', message: '見つかりません' } });
  }
  return send(res, 404, { error: { code: 'NOT_FOUND', message: '見つかりません' } });
}).listen(port, '127.0.0.1', () => console.log(`mock public API: http://127.0.0.1:${port} (${all.length}件)`));
