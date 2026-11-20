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
  r = await get(api, '/api/v1/auth/me'); check('api /api/v1/auth/me は未認証で401（JSONエラー）', r?.status === 401 && (await r.json().catch(() => ({})))?.error?.code === 'UNAUTHENTICATED');
  r = await fetch(`http://127.0.0.1:${api}/api/v1/auth/login`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{"loginId":"x","password":"y"}' }).catch(() => null);
  check('api ログインはOrigin無しだと403', r?.status === 403);
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

  // 認証のE2E: 初期OwnerのtokenでパスワードURL受諾→ログイン→ユーザー作成→その招待を受諾→ログイン→停止→ログイン不可
  {
    const origin = 'http://127.0.0.1:3000'; // composeの既定APP_BASE_URL（許可Origin）
    const call = async (method, path, { body, cookie, csrf, headers = {} } = {}) => {
      const res = await fetch(`http://127.0.0.1:${api}/api/v1${path}`, {
        method, redirect: 'manual',
        headers: { 'content-type': 'application/json', origin, ...(cookie ? { cookie } : {}), ...(csrf ? { 'x-csrf-token': csrf } : {}), ...headers },
        body: body ? JSON.stringify(body) : undefined,
      });
      const text = await res.text();
      return { status: res.status, json: text ? JSON.parse(text) : null, setCookie: res.headers.getSetCookie?.() ?? [] };
    };
    const ownerTok = b1.stdout.split('\n').map(l => l.trim()).find(l => /^[A-Za-z0-9_-]{43}$/.test(l));
    const pw = 'smoke-passphrase-owner-1', pw2 = 'smoke-passphrase-member-2';
    let x = await call('POST', '/auth/accept-invite', { body: { token: ownerTok, password: pw } });
    check('E2E 初期Ownerが招待tokenでパスワードを設定', x.status === 204, `status=${x.status}`);
    x = await call('POST', '/auth/login', { body: { loginId: 'smoke.owner', password: pw } });
    const cookie = (x.setCookie[0] ?? '').split(';')[0];
    check('E2E Ownerがログインでき、セッションCookieとcsrfTokenを得る', x.status === 200 && cookie.startsWith('nu_session=') && !!x.json?.csrfToken, `status=${x.status}`);
    const csrf = x.json?.csrfToken;
    x = await call('POST', '/admin/users', { body: { loginId: 'smoke.member', displayName: 'スモーク会員(架空)' }, cookie, csrf });
    const invite = x.json?.url?.split('#token=')[1];
    check('E2E Ownerがユーザーを作成し招待URL（フラグメント）を得る', x.status === 201 && !!invite && x.json.url.includes('/invite#token='), `status=${x.status}`);
    x = await call('POST', '/admin/users', { body: { loginId: 'smoke.member2', displayName: 'x' }, cookie });
    check('E2E CSRFトークン無しの管理操作は403', x.status === 403, `status=${x.status}`);
    x = await call('POST', '/auth/accept-invite', { body: { token: invite, password: pw2 } });
    check('E2E 作成したユーザーが招待を受諾', x.status === 204, `status=${x.status}`);
    x = await call('POST', '/auth/login', { body: { loginId: 'smoke.member', password: pw2 } });
    const mcookie = (x.setCookie[0] ?? '').split(';')[0];
    check('E2E 作成したユーザーがログインできる', x.status === 200 && !!mcookie, `status=${x.status}`);
    x = await call('POST', '/admin/users', { body: { loginId: 'smoke.nope', displayName: 'x' }, cookie: mcookie, csrf: x.json?.csrfToken });
    check('E2E 一般メンバーの管理操作は403', x.status === 403, `status=${x.status}`);
    const memberId = (await call('GET', '/auth/me', { cookie: mcookie })).json?.user?.id;
    x = await call('PATCH', `/admin/users/${memberId}/status`, { body: { status: 'disabled' }, cookie, csrf });
    check('E2E Ownerがユーザーを停止', x.status === 200 && x.json?.status === 'disabled', `status=${x.status}`);
    x = await call('GET', '/auth/me', { cookie: mcookie });
    check('E2E 停止後、既存セッションは使えない', x.status === 401, `status=${x.status}`);
    x = await call('POST', '/auth/login', { body: { loginId: 'smoke.member', password: pw2 } });
    check('E2E 停止後はログインできない', x.status === 401, `status=${x.status}`);
    x = await call('POST', '/auth/login', { body: { loginId: 'smoke.owner', password: pw }, headers: { origin: 'https://evil.example' } });
    check('E2E 他のOriginからのログインは403', x.status === 403, `status=${x.status}`);
  }

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
