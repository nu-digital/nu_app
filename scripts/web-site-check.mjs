// 公式サイト（Next standalone）を、擬似の公開API（scripts/mock-public-api.mjs）と組み合わせて起動し、検査する。
//   前提: `API_INTERNAL_URL=http://127.0.0.1:18080 npm run web:build` 済み・PLAYWRIGHT_DIR にPlaywright（既定 apps/web）。
//   検査: ① web-verify（全ルート・記事・画像）② ブラウザー操作（e2e: 全ルート・ギャラリー・横溢れ）
//        ③ 基準との指紋の比較（意図した差は docs/legacy-baseline/known-diffs.json に理由つきで列挙）
import { spawn, spawnSync } from 'node:child_process';
import { cpSync, existsSync, rmSync, mkdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(fileURLToPath(new URL('..', import.meta.url)));
const web = join(repo, 'apps/web');
const apiPort = 18080, webPort = 3100;
const standalone = join(web, '.next/standalone');
if (!existsSync(join(standalone, 'server.js'))) { console.error('FAIL: .next/standalone がありません。先に web:build を実行してください'); process.exit(1); }
// standalone出力には .next/static と public が含まれないため、実行用に配置する（Dockerfileと同じ）
rmSync(join(standalone, '.next/static'), { recursive: true, force: true });
rmSync(join(standalone, 'public'), { recursive: true, force: true });
mkdirSync(join(standalone, '.next'), { recursive: true });
cpSync(join(web, '.next/static'), join(standalone, '.next/static'), { recursive: true });
cpSync(join(web, 'public'), join(standalone, 'public'), { recursive: true });

const children = [];
const start = (cmd, args, env) => { const c = spawn(cmd, args, { cwd: repo, env: { ...process.env, ...env }, stdio: ['ignore', 'inherit', 'inherit'] }); children.push(c); return c; };
const stop = () => { for (const c of children) c.kill('SIGTERM'); };
process.on('exit', stop);
const waitFor = async (url, ms = 30000) => {
  const t = Date.now();
  while (Date.now() - t < ms) { try { if ((await fetch(url)).ok) return; } catch { /* 起動待ち */ } await new Promise(r => setTimeout(r, 300)); }
  throw new Error(`起動しません: ${url}`);
};
const run = (name, cmd, args, env = {}) => {
  console.log(`\n== ${name}`);
  const r = spawnSync(cmd, args, { cwd: repo, env: { ...process.env, ...env }, stdio: 'inherit' });
  if (r.status !== 0) { console.error(`FAIL: ${name}`); process.exit(1); }
};

start('node', ['scripts/mock-public-api.mjs', '--port', String(apiPort)], {});
await waitFor(`http://127.0.0.1:${apiPort}/healthz`);
start('node', [join(standalone, 'server.js')], { PORT: String(webPort), HOSTNAME: '127.0.0.1', API_INTERNAL_URL: `http://127.0.0.1:${apiPort}`, NODE_ENV: 'production' });
await waitFor(`http://127.0.0.1:${webPort}/healthz/`);
const site = `http://127.0.0.1:${webPort}`;
const pw = { PLAYWRIGHT_DIR: process.env.PLAYWRIGHT_DIR ?? web };
run('web-verify', 'node', ['scripts/web-verify.mjs', site]);
run('ブラウザー操作（e2e）', 'node', ['scripts/legacy-baseline-capture.mjs', site, process.env.E2E_OUT_DIR ?? join(web, '.e2e-out')], pw);
run('基準との指紋の比較', 'node', ['scripts/legacy-fingerprint-capture.mjs', '--base', site, '--compare', 'docs/legacy-baseline/pages.json', '--known', 'docs/legacy-baseline/known-diffs.json'], pw);
// ④ 公開APIが使えないとき: コンテンツのページは5xxで`no-store`（エラーページをキャッシュさせない）。ヘルスチェックはAPIに依存せず200
console.log('\n== 公開APIが使えないときの挙動');
start('node', [join(standalone, 'server.js')], { PORT: '3101', HOSTNAME: '127.0.0.1', API_INTERNAL_URL: 'http://127.0.0.1:1', NODE_ENV: 'production' });
await waitFor('http://127.0.0.1:3101/healthz/');
const bad = [];
for (const p of ['/', '/blog/', '/project/', '/blog/another/']) {
  const r = await fetch(`http://127.0.0.1:3101${p}`);
  const cc = r.headers.get('cache-control') ?? '';
  if (r.status < 500 || !/no-store/.test(cc)) bad.push(`${p}: status=${r.status} cache-control=${cc}`);
}
if ((await fetch('http://127.0.0.1:3101/healthz/')).status !== 200) bad.push('healthz');
if (bad.length) { console.error(`FAIL: APIが使えないときの挙動\n${bad.join('\n')}`); process.exit(1); }
console.log('APIが使えないとき: コンテンツのページは5xx・no-store、healthzは200');
console.log('\nPASS: web-site-check');
stop();
process.exit(0);
