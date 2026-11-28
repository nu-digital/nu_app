// 管理ポータルを、開発用モックAPIつきで起動する（Docker・Go・DB不要。Windows・macOS・Linux共通）。
//   npm run admin:dev:mock            → http://127.0.0.1:3000/login を開き、owner / admin / editor / member でログイン
//   npm run admin:dev:mock -- --port 3100 --mock-port 18391
// モック（apps/admin/mock/server.ts）とadminの next dev を子プロセスで起動し、Ctrl+C で両方止める。
// adminの /api/v1 の中継先（API_INTERNAL_URL）をモックに向けるだけで、アプリのコードにはモックの分岐を入れない。
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(fileURLToPath(new URL('..', import.meta.url)));
const admin = join(repo, 'apps/admin');
const arg = (n, d) => { const i = process.argv.indexOf(`--${n}`); return i < 0 ? d : process.argv[i + 1]; };
const port = arg('port', '3000');
const mockPort = arg('mock-port', '18390');
const nextBin = join(admin, 'node_modules/next/dist/bin/next');

const [major, minor] = process.versions.node.split('.').map(Number);
if (major < 22 || (major === 22 && minor < 18)) {
  console.error(`Node ${process.versions.node} ではモック（TypeScript）を実行できません。.node-version の版（22.18以上）を使ってください。`);
  process.exit(1);
}
if (!existsSync(nextBin)) {
  console.error('apps/admin の依存が未導入です。先に `npm run admin:ci` を実行してください。');
  process.exit(1);
}

const children = [];
const run = (label, args, env) => {
  const c = spawn(process.execPath, args, { cwd: admin, env: { ...process.env, ...env }, stdio: 'inherit' });
  c.on('exit', (code) => { console.log(`[${label}] 終了しました（code=${code}）`); stop(code ?? 0); });
  children.push(c);
};
let stopping = false;
function stop(code) {
  if (stopping) return;
  stopping = true;
  for (const c of children) if (c.exitCode === null) c.kill();
  setTimeout(() => process.exit(code), 300);
}
process.on('SIGINT', () => stop(0));
process.on('SIGTERM', () => stop(0));

run('mock', [join(admin, 'mock/server.ts'), '--port', mockPort], { NODE_ENV: 'development' });
run('admin', [nextBin, 'dev', '--hostname', '127.0.0.1', '--port', port], { API_INTERNAL_URL: `http://127.0.0.1:${mockPort}`, WEB_INTERNAL_URL: `http://127.0.0.1:${mockPort}/web` });
console.log(`\n管理ポータル（モック）: http://127.0.0.1:${port}/login\n`);
