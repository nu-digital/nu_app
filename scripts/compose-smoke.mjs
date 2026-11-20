// ローカルComposeの起動smoke。専用のプロジェクト名(nu_app_smoke)・架空の環境値・空きポートで起動し、
// 確認後にそのプロジェクトだけを volume ごと削除する。開発用プロジェクト(nu_app)には触れない。
// 使い方: node scripts/compose-smoke.mjs [--keep]
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const PROJECT = 'nu_app_smoke';
const keep = process.argv.includes('--keep');
const inv = JSON.parse(readFileSync(join(root, 'docs/legacy-baseline/inventory.json'), 'utf8'));

const freePort = () => new Promise(res => { const s = createServer(); s.listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => res(p)); }); });
const [db, api, web, admin] = [await freePort(), await freePort(), await freePort(), await freePort()];
const dir = mkdtempSync(join(tmpdir(), 'nu-app-smoke-'));
const envFile = join(dir, 'env');
writeFileSync(envFile, `POSTGRES_USER=smoke\nPOSTGRES_DB=smoke\nPOSTGRES_PASSWORD=smoke-only-not-secret\nDB_PORT=${db}\nAPI_PORT=${api}\nWEB_PORT=${web}\nADMIN_PORT=${admin}\n`);
const compose = (...args) => spawnSync('docker', ['compose', '-p', PROJECT, '--env-file', envFile, '-f', join(root, 'infrastructure/compose.yaml'), ...args], { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });

const failures = [];
const check = (name, ok, detail = '') => { console.log(`${ok ? 'OK ' : 'NG '} ${name}${detail ? ' — ' + detail : ''}`); if (!ok) failures.push(name); };
const get = async (port, path) => { try { return await fetch(`http://127.0.0.1:${port}${path}`); } catch { return null; } };

try {
  const up = compose('up', '--build', '-d', '--wait', '--wait-timeout', '240');
  check('compose up --wait', up.status === 0, up.status === 0 ? '' : (up.stderr || '').slice(-1200));
  const ps = JSON.parse('[' + compose('ps', '-a', '--format', 'json').stdout.trim().split('\n').filter(Boolean).join(',') + ']');
  const byName = Object.fromEntries(ps.map(p => [p.Service, p]));
  for (const s of ['db', 'api', 'web', 'admin']) check(`${s} がhealthy`, byName[s]?.Health === 'healthy', byName[s]?.Health ?? 'なし');
  check('migrate が正常終了', byName.migrate?.State === 'exited' && byName.migrate?.ExitCode === 0);
  check('bot が実行中', byName.bot?.State === 'running');
  const hostBinds = ps.flatMap(p => p.Publishers ?? []).filter(x => x.PublishedPort);
  check('公開ポートは127.0.0.1のみ', hostBinds.length > 0 && hostBinds.every(x => x.URL === '127.0.0.1'), hostBinds.map(x => `${x.URL}:${x.PublishedPort}`).join(','));

  let r = await get(api, '/healthz'); check('api /healthz 200', r?.status === 200);
  r = await get(api, '/readyz'); check('api /readyz 200（DB到達）', r?.status === 200, JSON.stringify(await r?.json().catch(() => null)));
  r = await get(admin, '/healthz'); check('admin /healthz 200', r?.status === 200);
  const bad = [];
  for (const route of inv.routes.filter(x => !['/404/', '/_not-found/'].includes(x))) { const x = await get(web, route); if (x?.status !== 200) bad.push(`${route}:${x?.status}`); }
  check(`web 全${inv.routes.length - 2}ルート200`, bad.length === 0, bad.slice(0, 3).join(','));
  r = await get(web, '/no-such-page/'); check('web 未定義URLは404', r?.status === 404);

  // 初期Owner作成CLI: 1回目は成功してtokenを表示、2回目は別IDでも拒否(終了コード3)でtokenを表示しない
  const boot = (id) => compose('run', '--rm', '--no-deps', '--entrypoint', '/nuctl', 'api', 'bootstrap-owner', '--login-id', id, '--display-name', 'スモーク(架空)');
  const b1 = boot('smoke.owner');
  check('nuctl bootstrap-owner 1回目は成功しtokenを表示', b1.status === 0 && /^[A-Za-z0-9_-]{43}$/m.test(b1.stdout), `exit=${b1.status}`);
  const b2 = boot('smoke.second');
  check('nuctl bootstrap-owner 2回目は拒否(終了コード3)', b2.status === 3 && !/^[A-Za-z0-9_-]{43}$/m.test(b2.stdout), `exit=${b2.status}`);

  const stopDb = compose('stop', 'db');
  check('db停止', stopDb.status === 0);
  r = await get(api, '/readyz'); check('DB停止後 api /readyz は503', r?.status === 503);
  r = await get(api, '/healthz'); check('DB停止後 api /healthz は200のまま', r?.status === 200);

  const users = ['api', 'bot', 'web', 'admin'].map(s => {
    const id = byName[s]?.ID;
    return [s, id ? execFileSync('docker', ['inspect', '--format', '{{.Config.User}}', id], { encoding: 'utf8' }).trim() : ''];
  });
  check('api/bot/web/adminは非root設定', users.every(([, u]) => u && !['0', 'root'].includes(u.split(':')[0])), users.map(([s, u]) => `${s}=${u}`).join(' '));
} finally {
  if (failures.length) console.log((compose('logs', '--no-color', '--tail', '40').stdout || '').slice(-3000));
  if (!keep) compose('down', '-v', '--remove-orphans');
  rmSync(dir, { recursive: true, force: true });
}
console.log(failures.length ? `FAIL: ${failures.length}件 (${failures.join(', ')})` : 'PASS: Compose smoke');
process.exit(failures.length ? 1 : 0);
