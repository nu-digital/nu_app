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
  r = await get(web, '/no-such-page/'); check('web 未定義URLは404', r?.status === 404);
  r = await get(web, '/healthz/'); check('web /healthz/ 200（公開APIに依存しない）', r?.status === 200);

  // 初期Owner作成CLI: 1回目は成功してtokenを表示、2回目は別IDでも拒否(終了コード3)でtokenを表示しない
  const boot = (id) => compose('run', '--rm', '--no-deps', '--entrypoint', '/nuctl', 'api', 'bootstrap-owner', '--login-id', id, '--display-name', 'スモーク(架空)');
  const b1 = boot('smoke.owner');
  check('nuctl bootstrap-owner 1回目は成功しtokenを表示', b1.status === 0 && /^[A-Za-z0-9_-]{43}$/m.test(b1.stdout), `exit=${b1.status}`);
  const b2 = boot('smoke.second');
  check('nuctl bootstrap-owner 2回目は拒否(終了コード3)', b2.status === 3 && !/^[A-Za-z0-9_-]{43}$/m.test(b2.stdout), `exit=${b2.status}`);

  // 認証のE2E: 初期OwnerのtokenでパスワードURL受諾→ログイン→ユーザー作成→その招待を受諾→ログイン→停止→ログイン不可
  {
    const origin = `http://127.0.0.1:${admin}`; // composeの既定APP_BASE_URL（=adminのURL。許可Origin）
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


    // メディア（画像）: 実際のスタックで、Nextの中継経由の大きな画像・公開境界・永続ボリューム・運用CLIを確かめる
    // 既存記事の移行（運用CLI。サーバー上で手動実行する手順と同じ）。CMSで記事を作る前（画面E2Eが作る公開記事が基準比較に混ざらない）に行う→ 公式サイトが公開APIから描画 → 移設前基準との一致（P3-04c）
    {
      const imp = (extra = []) => compose('run', '--rm', '--no-deps', '-v', `${root}/apps/web/content:/content:ro`, '-v', `${root}/docs/legacy-baseline:/baseline:ro`, '--entrypoint', '/nuctl', 'api', 'import-legacy', '--content-dir', '/content', '--baseline-dir', '/baseline', '--as', 'smoke.owner', ...extra);
      const dry = imp();
      check('移行: dry-runは何も書かずに成功する', dry.status === 0, `exit=${dry.status} ${(dry.stderr || '').slice(-300)}`);
      const apply = imp(['--apply']);
      check('移行: nuctl import-legacy --apply で既存67記事を取り込める', apply.status === 0 && /67/.test(apply.stdout), `exit=${apply.status} ${(apply.stderr || '').slice(-300)}`);
      const again = imp(['--apply']);
      check('移行: 再実行しても成功し、重複して作らない（冪等）', again.status === 0, `exit=${again.status} ${(again.stderr || '').slice(-300)}`);
      const bad = [];
      for (const route of inv.routes.filter(x => !['/404/', '/_not-found/'].includes(x))) {
      let x = await get(web, route);
      for (let t = 0; !x && t < 3; t++) { await new Promise(res => setTimeout(res, 1000)); x = await get(web, route); } // 接続の一時的な失敗だけ再試行する（ステータスは再試行しない）
      if (x?.status !== 200) bad.push(`${route}:${x?.status}`);
    }
      check(`web 全${inv.routes.length - 2}ルート200（実際の公開APIから描画）`, bad.length === 0, bad.slice(0, 3).join(','));
      const site = `http://127.0.0.1:${web}`;
      const ver = spawnSync('node', ['scripts/web-verify.mjs', site], { cwd: root, encoding: 'utf8' });
      check('web-verify: 全ルート・記事・画像が移設前基準と一致', ver.status === 0, (ver.stdout + ver.stderr).slice(-400));
      if (process.env.PLAYWRIGHT_SKIP !== '1') {
        const cmp = spawnSync('node', ['scripts/legacy-fingerprint-capture.mjs', '--base', site, '--compare', 'docs/legacy-baseline/pages.json', '--known', 'docs/legacy-baseline/known-diffs.json'], { cwd: root, encoding: 'utf8', env: { ...process.env, PLAYWRIGHT_DIR: join(root, 'apps/admin') } });
        check('指紋: 実際のAPI・実際の移行結果が、移設前基準と一致（意図した差を除く）', cmp.status === 0, (cmp.stdout + cmp.stderr).slice(-3500));
      }
      // 投稿者は公開許可が確認されるまで「nu_digital」。名前は公開APIにも公式サイトのHTMLにも出ない
      const api1 = await (await get(api, '/api/v1/public/contents?limit=100'))?.text();
      check('移行: 公開APIに、未確認の投稿者名が出ない', !!api1 && !/"author":"(?!nu_digital)/.test(api1));
      const html = await (await get(web, '/blog/another/'))?.text();
      check('移行: 公式サイトの記事に「投稿者：nu_digital」と表示される', !!html && /投稿者：(<!-- -->)?nu_digital/.test(html));
    }
    {
      const { randomBytes } = await import('node:crypto');
      const zlib = await import('node:zlib');
      const pngChunk = (type, data) => {
        const len = Buffer.alloc(4); len.writeUInt32BE(data.length);
        const crc = Buffer.alloc(4); crc.writeUInt32BE(zlib.crc32(Buffer.concat([Buffer.from(type), data])) >>> 0);
        return Buffer.concat([len, Buffer.from(type), data, crc]);
      };
      // 圧縮しにくい（乱数の）画素のPNGを作る。w×h×3バイトの生データをdeflate(level 0)で包む → 約9MB
      const noisePng = (w, h) => {
        const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2;
        const raw = Buffer.alloc((w * 3 + 1) * h);
        for (let y = 0; y < h; y++) { randomBytes(w * 3).copy(raw, y * (w * 3 + 1) + 1); }
        return Buffer.concat([Buffer.from('89504e470d0a1a0a', 'hex'), pngChunk('IHDR', ihdr), pngChunk('IDAT', zlib.deflateSync(raw, { level: 0 })), pngChunk('IEND', Buffer.alloc(0))]);
      };
      const big = noisePng(1500, 2000);
      check(`メディア: 試験用の画像が9MB台で10MiB以内（${(big.length / 1048576).toFixed(2)}MiB）`, big.length > 8.5 * 1048576 && big.length < 10 * 1048576);
      const post = (base, body, ct = 'image/png') => fetch(`${base}/api/v1/media?purpose=cms`, { method: 'POST', headers: { 'content-type': ct, origin, cookie, 'x-csrf-token': csrf }, body });
      let r = await post(`http://127.0.0.1:${api}`, big);
      const direct = await r.json().catch(() => ({}));
      check('メディア: APIへ直接、約9MBの画像をアップロードできる（再エンコードされる）', r.status === 201 && direct.mimeType === 'image/webp' && direct.width === 1500, `status=${r.status}`);
      r = await post(`http://127.0.0.1:${admin}`, big);
      const viaProxy = await r.json().catch(() => ({}));
      check('メディア: adminの中継経由でも約9MBの画像をアップロードできる', r.status === 201 && viaProxy.publicId?.length === 22, `status=${r.status}`);
      // 上限（10MiB）をわずかに超える本文は、中継を通ってAPIまで届き、APIが413で拒否する（中継が10MiBで本文を切ると、切れた画像として別の応答になる）
      const over = Buffer.concat([big, Buffer.alloc(10 * 1048576 + 1000 - big.length, 0)]);
      r = await post(`http://127.0.0.1:${admin}`, over);
      const overBody = await r.json().catch(() => ({}));
      check('メディア: 上限を超える本文は、adminの中継経由でもAPIが413で拒否する（中継の本文上限を引き上げている）', r.status === 413 && overBody.error?.code === 'PAYLOAD_TOO_LARGE', `status=${r.status} code=${overBody.error?.code}`);
      r = await post(`http://127.0.0.1:${api}`, Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>'), 'image/png');
      check('メディア: SVGは415', r.status === 415, `status=${r.status}`);
      const get = (base, path, withCookie = true) => fetch(`${base}${path}`, { headers: withCookie ? { cookie } : {} });
      r = await get(`http://127.0.0.1:${api}`, `/api/v1/public/media/${direct.publicId}`, false);
      check('メディア: 参照されていない画像は匿名に配信されない（404）', r.status === 404, `status=${r.status}`);
      r = await get(`http://127.0.0.1:${api}`, `/api/v1/media/${direct.id}`);
      check('メディア: アップロード者は認証つきで取得でき、nosniff・privateのヘッダー', r.status === 200 && r.headers.get('content-type') === 'image/webp' && r.headers.get('x-content-type-options') === 'nosniff' && /no-store/.test(r.headers.get('cache-control') ?? ''), `status=${r.status}`);
      x = await call('POST', '/contents', { body: { type: 'article', section: 'blog', slug: 'smoke-media', title: '画像つきの記事(架空)', markdown: `![x](/api/v1/public/media/${direct.publicId})`, coverMediaId: viaProxy.id }, cookie, csrf });
      check('メディア: 画像を本文とカバーで参照する記事を作れる', x.status === 201, `status=${x.status}`);
      const cid = x.json?.id;
      x = await call('POST', `/contents/${cid}/publish`, { body: { revisionNo: 1 }, cookie, csrf });
      check('メディア: 記事を公開できる', x.status === 200, `status=${x.status}`);
      r = await get(`http://127.0.0.1:${api}`, `/api/v1/public/media/${direct.publicId}`, false);
      const pubBody = Buffer.from(await r.arrayBuffer());
      check('メディア: 公開後は匿名に配信される（画像として読めてCORPはcross-origin）', r.status === 200 && pubBody.subarray(0, 4).toString() === 'RIFF' && pubBody.subarray(8, 12).toString() === 'WEBP' && r.headers.get('cross-origin-resource-policy') === 'cross-origin', `status=${r.status}`);
      // 永続ボリューム: APIコンテナを再起動しても画像が残る
      const restart = compose('restart', 'api');
      for (let i = 0; i < 40; i++) { if ((await get(`http://127.0.0.1:${api}`, '/healthz', false).catch(() => ({ status: 0 }))).status === 200) break; await new Promise(res => setTimeout(res, 1000)); }
      r = await get(`http://127.0.0.1:${api}`, `/api/v1/public/media/${direct.publicId}`, false);
      check('メディア: APIコンテナを再起動しても公開画像が残る（永続ボリューム）', restart.status === 0 && r.status === 200, `restart=${restart.status} status=${r.status}`);
      x = await call('POST', `/contents/${cid}/unpublish`, { cookie, csrf });
      r = await get(`http://127.0.0.1:${api}`, `/api/v1/public/media/${direct.publicId}`, false);
      check('メディア: 公開停止すると画像はすぐ404になる', x.status === 200 && r.status === 404, `status=${r.status}`);
      const verify = compose('run', '--rm', '--no-deps', '--entrypoint', '/nuctl', 'api', 'media-verify');
      check('メディア: コンテナ内の運用CLI(media-verify)で、行とファイルの食い違いが無い', verify.status === 0 && /食い違いはありません/.test(verify.stdout) && /有効な画像: 2 件/.test(verify.stdout), `status=${verify.status} ${verify.stdout.slice(0, 200)}`);
    }

    // 画面E2E（Playwright）: ブラウザーからadminの画面を操作する。PLAYWRIGHT_SKIP=1で省略できる（ブラウザー未導入の環境用。CIでは実行する）
    if (process.env.PLAYWRIGHT_SKIP === '1') {
      console.log('SKIP 画面E2E（PLAYWRIGHT_SKIP=1。実行していない）');
    } else {
      // 一般メンバー(smoke.member)はすでに停止したため、画面確認用に新しいユーザーを作り直す
      const created = await call('POST', '/admin/users', { body: { loginId: 'ui.login.member', displayName: 'UI会員(架空)' }, cookie, csrf });
      const t2 = created.json?.url?.split('#token=')[1];
      await call('POST', '/auth/accept-invite', { body: { token: t2, password: 'ui-login-passphrase-1' } });
      const { runAdminUiE2E } = await import('./admin-ui-e2e.mjs');
      await runAdminUiE2E({
        adminUrl: `http://127.0.0.1:${admin}`, apiCall: call, ownerLogin: 'smoke.owner', ownerPass: pw, ownerCookie: cookie, ownerCsrf: csrf,
        memberLogin: 'ui.login.member', memberPass: 'ui-login-passphrase-1', memberName: 'UI会員(架空)',
        shotsDir: process.env.UI_SHOTS_DIR ?? join(root, '.local/ui-shots'), check,
        // 使い捨てのsmoke用DB（本番ではない）へSQLを流す。APIでは作れない状態（送信に失敗して止まった通知）を用意するために使う
        dbExec: (sql) => { const r = compose('exec', '-T', 'db', 'psql', '-U', 'smoke', '-d', 'smoke', '-v', 'ON_ERROR_STOP=1', '-c', sql); if (r.status !== 0) throw new Error('dbExec失敗: ' + (r.stderr || '').slice(0, 300)); },
      });
    }

    // 契約: 開発用モック（apps/admin/mock）の応答の形が、実APIと一致する（E2Eでデータが入った状態で比べる）
    {
      const { CONTRACT_PATHS, compareShapes } = await import('./admin-mock-contract.mjs');
      const { createMock } = await import('../apps/admin/mock/handler.ts');
      const { OPTIONAL_KEYS } = await import('../apps/admin/mock/fixtures.ts');
      const mock = createMock();
      x = await call('POST', '/auth/login', { body: { loginId: 'smoke.owner', password: pw } });
      const oc = (x.setCookie[0] ?? '').split(';')[0];
      const realFirst = async (path) => (await call('GET', path, { cookie: oc })).json?.items?.[0]?.id;
      const mockFirst = (path) => mock('GET', '/api/v1' + path, 'owner').json?.items?.[0]?.id;
      const listOf = { member: '/members?limit=1', item: '/inventory/items?limit=1', content: '/contents?limit=1' };
      const realIds = {}, mockIds = {};
      for (const [k, path] of Object.entries(listOf)) { realIds[k] = await realFirst(path); mockIds[k] = mockFirst(path); }
      const diffs = [], skipped = [];
      for (const path of CONTRACT_PATHS) {
        const key = path.match(/:(w+)/)?.[1];
        if (key && !realIds[key]) { skipped.push(path); continue; }
        const real = await call('GET', path.replace(/:(w+)/, (_, k) => realIds[k]), { cookie: oc });
        const m = mock('GET', '/api/v1' + path.replace(/:(w+)/, (_, k) => mockIds[k]), 'owner');
        if (real.status !== 200 || m.status !== 200) { diffs.push(`${path}: status 実API=${real.status} モック=${m.status}`); continue; }
        diffs.push(...compareShapes(m.json, real.json, OPTIONAL_KEYS, path));
      }
      check(`契約: 開発用モックと実APIの応答の形が一致（${CONTRACT_PATHS.length - skipped.length}件を比較${skipped.length ? '、比較できず: ' + skipped.join(',') : ''}）`, diffs.length === 0, diffs.slice(0, 12).join(' / '));
    }
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
