// 内部アプリ(apps/admin)の画面E2E。実ブラウザー(Playwright)で、ログイン・ランチャー・招待受諾・再設定を操作する。
// compose-smoke.mjs から呼ばれる（起動済みのスタックに対して実行）。単体では実行しない。
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import { deflateSync } from 'node:zlib';
import { join, resolve } from 'node:path';

const require = createRequire(join(resolve('apps/admin'), 'x.js'));
const { chromium } = require('playwright');

/** 検査用の小さなPNG（単色。seedで色を変える）。画像のアップロードの確認に使う。 */
function makePng(w, h, seed) {
  const crcTable = Array.from({ length: 256 }, (_, n) => { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c >>> 0; });
  const crc = (buf) => { let c = 0xffffffff; for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0; };
  const chunk = (type, data) => { const len = Buffer.alloc(4); len.writeUInt32BE(data.length); const td = Buffer.concat([Buffer.from(type), data]); const c = Buffer.alloc(4); c.writeUInt32BE(crc(td)); return Buffer.concat([len, td, c]); };
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2;
  const row = Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: w }, (_, x) => [(seed * 70 + x * 3) % 256, (seed * 40 + 90) % 256, 200]).flat())]);
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}

/**
 * @param {{dbExec?:(sql:string)=>void, ownerLogin:string, ownerPass:string, adminUrl:string, apiCall:Function, ownerCookie:string, ownerCsrf:string, memberLogin:string, memberPass:string, memberName:string, shotsDir?:string, check:(name:string, ok:boolean, detail?:string)=>void}} o
 */
export async function runAdminUiE2E(o) {
  const { adminUrl, check } = o;
  if (o.shotsDir) mkdirSync(o.shotsDir, { recursive: true });
  const browser = await chromium.launch();
  try {
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
      const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const page = await ctx.newPage();
      const overflow = async () => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      const shot = async (name) => o.shotsDir && page.screenshot({ path: join(o.shotsDir, `${label}-${name}.png`) });

      // 未ログインでランチャーを開くとログインへ
      await page.goto(adminUrl + '/');
      await page.waitForURL('**/login');
      check(`[${label}] 未ログインでは/へ入れずログイン画面になる`, new URL(page.url()).pathname === '/login');
      check(`[${label}] ログイン画面に横溢れがない`, (await overflow()) <= 0, `overflow=${await overflow()}`);
      await page.getByLabel('ログインID').waitFor();
      // パスワード欄の目のアイコン: 枠の中に収まり、押すと表示・非表示が切り替わる（読み上げ名と押下状態つき）
      const pass = page.getByLabel('パスワード', { exact: true });
      const eye = page.getByRole('button', { name: 'パスワードを表示する' });
      const [passBox, eyeBox] = await Promise.all([pass.locator('xpath=ancestor::div[contains(@class,"charcoal-text-field-container")]').boundingBox(), eye.boundingBox()]);
      check(`[${label}] 目のアイコンがパスワード欄の枠内にある`, eyeBox && passBox && eyeBox.y >= passBox.y && eyeBox.y + eyeBox.height <= passBox.y + passBox.height + 1, JSON.stringify({ passBox, eyeBox }));
      await eye.click();
      check(`[${label}] 目のアイコンでパスワードを表示できる`, (await pass.getAttribute('type')) === 'text' && (await page.getByRole('button', { name: 'パスワードを隠す' }).getAttribute('aria-pressed')) === 'true');
      await page.getByRole('button', { name: 'パスワードを隠す' }).click();
      check(`[${label}] もう一度押すと隠れる`, (await pass.getAttribute('type')) === 'password');
      const brokenLogin = await page.evaluate(() => [...document.images].filter((i) => !i.complete || i.naturalWidth === 0).length);
      check(`[${label}] ログイン画面のロゴが読み込まれる`, brokenLogin === 0);
      await shot('login');

      // 誤ったパスワード: エラーが伝わり、フォーカスがエラーへ移り、入力は保持される
      await page.getByLabel('ログインID').fill(o.memberLogin);
      await page.getByLabel('パスワード', { exact: true }).fill('wrong-password-123');
      await page.getByRole('button', { name: 'ログイン', exact: true }).click();
      const alert = page.locator('.notice.error');
      await alert.waitFor();
      check(`[${label}] 誤入力でエラー文言が表示される（「エラー」の文字つき）`, /エラー/.test(await alert.innerText()) && /ログインIDまたはパスワードが違います/.test(await alert.innerText()));
      check(`[${label}] エラーにフォーカスが移る`, await alert.evaluate((el) => el === document.activeElement));
      check(`[${label}] 失敗してもログインIDの入力は保持される`, (await page.getByLabel('ログインID').inputValue()) === o.memberLogin);
      await shot('login-error');

      // キーボードだけでログイン: ID→Tab→パスワード→Enter
      await page.getByLabel('ログインID').focus();
      await page.keyboard.press('Control+A');
      await page.keyboard.type(o.memberLogin);
      await page.keyboard.press('Tab');
      await page.keyboard.press('Control+A');
      await page.keyboard.type(o.memberPass);
      await page.keyboard.press('Enter');
      await page.waitForURL(adminUrl + '/', { timeout: 15000 }).catch(async (e) => {
        console.log('DEBUG ログイン後に遷移しない: url=' + page.url() + ' body=' + (await page.locator('body').innerText()).slice(0, 300));
        throw e;
      });
      await page.getByRole('heading', { level: 1 }).waitFor();
      check(`[${label}] キーボードだけでログインできる`, true);
      const h1 = await page.getByRole('heading', { level: 1 }).innerText();
      check(`[${label}] ランチャーに表示名が出る`, h1.includes(o.memberName), h1);
      check(`[${label}] 全員共通のカードが5枚あり、すべての機能が開ける（準備中のカードは無い）`, (await page.locator('.cards li').count()) === 5 && (await page.getByText('準備中').count()) === 0);
      const broken = await page.evaluate(() => [...document.images].filter((i) => !i.complete || i.naturalWidth === 0).map((i) => i.getAttribute('src')));
      check(`[${label}] 画像（ロゴ・団体写真）がすべて読み込まれる`, broken.length === 0, broken.join(','));
      const wrapped = await page.getByRole('button', { name: 'ログアウト' }).evaluate((el) => el.getBoundingClientRect().height <= 60);
      check(`[${label}] ログアウトボタンが折り返さない`, wrapped);
      check(`[${label}] ランチャーに横溢れがない`, (await overflow()) <= 0, `overflow=${await overflow()}`);
      const hero = await page.locator('.hero img').boundingBox();
      check(`[${label}] 団体写真が大きく表示される（幅いっぱい・高さ ${label === 'pc' ? '300' : '200'}px以上）`, !!hero && hero.width >= width - 1 && hero.height >= (label === 'pc' ? 300 : 200), JSON.stringify(hero));
      const small = await page.locator('button, a.card, .toggle').evaluateAll((els) => els.filter((e) => { const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0 && r.height < 44; }).length);
      check(`[${label}] ボタン・リンクの高さが44px以上`, small === 0, `44px未満=${small}`);
      await shot('launcher');

      // ログアウト → ログイン画面。戻っても見られない
      await page.getByRole('button', { name: 'ログアウト' }).click();
      await page.waitForURL('**/login');
      await page.goto(adminUrl + '/');
      await page.waitForURL('**/login');
      check(`[${label}] ログアウト後はランチャーに入れない`, new URL(page.url()).pathname === '/login');
      await ctx.close();
    }

    // 招待受諾（PC/スマホで同じ操作）。新しいユーザーを作り、招待URL(フラグメント)を画面で開く。
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844]]) {
      const loginId = `ui.${label}.member`;
      const created = await o.apiCall('POST', '/admin/users', { body: { loginId, displayName: `UI確認用(${label})` }, cookie: o.ownerCookie, csrf: o.ownerCsrf });
      const inviteUrl = created.json?.url;
      check(`[${label}] 招待URLがフラグメント形式`, typeof inviteUrl === 'string' && inviteUrl.includes('/invite#token='));
      const tokenPart = inviteUrl.split('#')[1];
      const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP' });
      const page = await ctx.newPage();
      const requests = [];
      page.on('request', (r) => requests.push(r.url()));
      await page.goto(`${adminUrl}/invite#${tokenPart}`);
      await page.getByLabel('新しいパスワード', { exact: true }).waitFor();
      check(`[${label}] 招待画面を開くとトークンがURLから消える`, !page.url().includes('token'), page.url());
      check(`[${label}] トークンがネットワークのURLに載らない`, requests.every((u) => !u.includes(tokenPart.slice(6))));
      await page.getByLabel('新しいパスワード', { exact: true }).fill('ui-passphrase-ok-1');
      await page.getByLabel('新しいパスワード（確認）').fill('ui-passphrase-different');
      await page.getByRole('button', { name: 'パスワードを設定する' }).click();
      check(`[${label}] 確認用が違うと送らずにエラー`, /一致しません/.test(await page.locator('.notice.error').innerText()));
      await page.getByLabel('新しいパスワード', { exact: true }).fill('short');
      await page.getByLabel('新しいパスワード（確認）').fill('short');
      await page.getByRole('button', { name: 'パスワードを設定する' }).click();
      await page.waitForFunction(() => /10文字以上/.test(document.querySelector('.notice.error')?.textContent ?? ''));
      check(`[${label}] 方針違反はサーバーの文言で表示され、入力が保持される`, (await page.getByLabel('新しいパスワード', { exact: true }).inputValue()) === 'short');
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-invite-error.png`) });
      await page.getByLabel('新しいパスワード', { exact: true }).fill('ui-passphrase-ok-1');
      await page.getByLabel('新しいパスワード（確認）').fill('ui-passphrase-ok-1');
      await page.getByRole('button', { name: 'パスワードを設定する' }).click();
      await page.getByText('完了').waitFor();
      check(`[${label}] 設定完了が表示されログインへの案内が出る`, (await page.getByRole('link', { name: 'ログイン画面へ' }).count()) === 1);
      await page.getByRole('link', { name: 'ログイン画面へ' }).click();
      await page.getByLabel('ログインID').fill(loginId);
      await page.getByLabel('パスワード', { exact: true }).fill('ui-passphrase-ok-1');
      await page.getByRole('button', { name: 'ログイン', exact: true }).click();
      await page.waitForURL(adminUrl + '/');
      check(`[${label}] 設定したパスワードでログインできる`, (await page.getByRole('heading', { level: 1 }).innerText()).includes(`UI確認用(${label})`));
      await ctx.close();
      // 使用済みのリンクを再度開くとエラー
      const ctx2 = await browser.newContext({ viewport: { width, height } });
      const p2 = await ctx2.newPage();
      await p2.goto(`${adminUrl}/invite#${tokenPart}`);
      await p2.getByLabel('新しいパスワード', { exact: true }).fill('another-passphrase-22');
      await p2.getByLabel('新しいパスワード（確認）').fill('another-passphrase-22');
      await p2.getByRole('button', { name: 'パスワードを設定する' }).click();
      await p2.locator('.notice.error').waitFor();
      check(`[${label}] 使用済みの招待リンクはエラーになる`, /無効|期限切れ/.test(await p2.locator('.notice.error').innerText()));
      await ctx2.close();
    }
    // ユーザー管理画面（Ownerが操作。PC・スマホ・320px）
    const login = async (page, id, pass) => {
      await page.goto(adminUrl + '/login');
      await page.getByLabel('ログインID').fill(id);
      await page.getByLabel('パスワード', { exact: true }).fill(pass);
      await page.getByRole('button', { name: 'ログイン', exact: true }).click();
      await page.waitForURL(adminUrl + '/');
    };
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
      const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const page = await ctx.newPage();
      await login(page, o.ownerLogin, o.ownerPass);
      // ランチャーの「ユーザー管理」カードから入る
      await page.getByRole('link', { name: /ユーザー管理/ }).click();
      await page.waitForURL('**/admin/users');
      await page.getByRole('heading', { level: 1, name: 'ユーザー管理' }).waitFor();
      const row = (id) => page.locator(`li.user[data-login="${id}"]`);
      await row(o.ownerLogin).waitFor();
      check(`[${label}] ユーザー一覧に自分が「（あなた）」つきで表示され、自分には停止ボタンが無い`, /（あなた）/.test(await row(o.ownerLogin).innerText()) && (await row(o.ownerLogin).getByRole('button', { name: '停止する' }).count()) === 0);
      const ovf = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      check(`[${label}] ユーザー管理に横溢れがない`, ovf <= 0, `overflow=${ovf}`);
      const small = await page.locator('button, a.btn, input, summary').evaluateAll((els) => els.filter((e) => { const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0 && r.height < 44; }).length);
      check(`[${label}] ユーザー管理のボタン・入力の高さが44px以上`, small === 0, `44px未満=${small}`);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-users.png`), fullPage: true });
      if (label === 'narrow') { await ctx.close(); continue; }

      // 検索
      await page.getByRole('searchbox').fill(o.ownerLogin);
      await page.getByRole('button', { name: '検索' }).click();
      await page.waitForFunction((n) => document.querySelectorAll('li.user').length === n, 1);
      check(`[${label}] 検索で絞り込める`, (await page.locator('li.user').count()) === 1);
      await page.getByRole('searchbox').fill('');
      await page.getByRole('button', { name: '検索' }).click();
      await page.waitForFunction(() => document.querySelectorAll('li.user').length > 1);

      // 追加 → 招待URLが一度だけ表示される
      const added = `ui.${label}.added`;
      await page.getByText('ユーザーを追加する', { exact: true }).click(); // 折りたたまれている追加フォームを開く
      await page.getByLabel('ログインID', { exact: true }).fill(added);
      await page.getByLabel('表示名', { exact: true }).fill(`追加確認(${label})`);
      await page.getByRole('button', { name: '追加して招待URLを発行する' }).click();
      await page.locator('#issued-url').waitFor();
      const url = await page.locator('#issued-url').inputValue();
      check(`[${label}] 追加すると招待URL（フラグメント形式）が表示される`, /\/invite#token=[A-Za-z0-9_-]{43}$/.test(url), url.replace(/token=.*/, 'token=…'));
      await row(added).waitFor();
      check(`[${label}] 追加したユーザーが「招待中」で一覧に出る`, /招待中/.test(await row(added).innerText()));
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-users-issued.png`) });
      await page.getByRole('button', { name: '閉じる' }).click();
      check(`[${label}] 閉じるとURLは画面から消える`, (await page.locator('#issued-url').count()) === 0);

      // 受諾（API）→ 一覧を更新すると「有効」
      const tok = url.split('#token=')[1];
      const acc = await o.apiCall('POST', '/auth/accept-invite', { body: { token: tok, password: 'ui-added-passphrase-1' } });
      check(`[${label}] 招待URLで受諾できる`, acc.status === 204, `status=${acc.status}`);
      await page.getByRole('button', { name: '検索' }).click();
      await page.waitForFunction((id) => /有効/.test(document.querySelector(`li.user[data-login="${id}"]`)?.textContent ?? ''), added);
      check(`[${label}] 受諾後は「有効」になる`, true);

      // 再設定URLの発行
      await row(added).getByRole('button', { name: '再設定URLを発行' }).click();
      await page.locator('#issued-url').waitFor();
      check(`[${label}] 再設定URLが発行される`, /\/reset#token=/.test(await page.locator('#issued-url').inputValue()));
      await page.getByRole('button', { name: '閉じる' }).click();

      // 停止（確認つき）→ 再開
      await row(added).getByRole('button', { name: '停止する' }).click();
      check(`[${label}] 停止には確認が出る`, (await row(added).getByRole('group').count()) === 1);
      await row(added).getByRole('button', { name: 'やめる' }).click();
      check(`[${label}] 「やめる」で停止されない`, /有効/.test(await row(added).innerText()));
      await row(added).getByRole('button', { name: '停止する' }).click();
      await row(added).getByRole('group').getByRole('button', { name: '停止する' }).click();
      await page.waitForFunction((id) => /停止中/.test(document.querySelector(`li.user[data-login="${id}"]`)?.textContent ?? ''), added);
      check(`[${label}] 停止すると「停止中」になる`, true);
      const re = await o.apiCall('POST', '/auth/login', { body: { loginId: added, password: 'ui-added-passphrase-1' } });
      check(`[${label}] 停止したユーザーはログインできない`, re.status === 401, `status=${re.status}`);
      await row(added).getByRole('button', { name: '再開する' }).click();
      await page.waitForFunction((id) => /有効/.test(document.querySelector(`li.user[data-login="${id}"]`)?.textContent ?? ''), added);
      check(`[${label}] 再開すると「有効」に戻る`, true);

      // Editorの付与・解除（Ownerなのでadminの付与ボタンも出る）
      await row(added).getByRole('button', { name: 'Editorにする' }).click();
      await page.waitForFunction((id) => /Editor/.test(document.querySelector(`li.user[data-login="${id}"] .badges`)?.textContent ?? ''), added);
      check(`[${label}] Editorを付与できる`, (await row(added).getByRole('button', { name: 'Adminにする' }).count()) === 1);
      await row(added).getByRole('button', { name: 'Editorを外す' }).click();
      await page.waitForFunction((id) => !/Editor/.test(document.querySelector(`li.user[data-login="${id}"] .badges`)?.textContent ?? ''), added);
      check(`[${label}] Editorを外せる`, true);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-users-after.png`), fullPage: true });
      await ctx.close();
    }
    // プロフィール・メンバー・個人情報（PC・スマホ・320px）
    const overflowOf = (page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    const smallOf = (page) => page.locator('button, a.btn, input:not([type=hidden]):not([type=checkbox]), label:has(input[type=checkbox]), select, textarea, summary').evaluateAll((els) => els.filter((e) => { const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0 && r.height < 44; }).length);
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
      const pid = `ui.${label}.prof`, pname = `プロフィール確認(${label})`, ppass = 'ui-profile-passphrase-1';
      const created = await o.apiCall('POST', '/admin/users', { body: { loginId: pid, displayName: `仮名(${label})` }, cookie: o.ownerCookie, csrf: o.ownerCsrf });
      await o.apiCall('POST', '/auth/accept-invite', { body: { token: created.json.url.split('#token=')[1], password: ppass } });
      const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const page = await ctx.newPage();
      await login(page, pid, ppass);
      await page.getByRole('link', { name: /^プロフィール/ }).click();
      await page.waitForURL('**/profile');
      await page.getByRole('heading', { level: 1, name: 'アカウント設定' }).waitFor();
      const visOf = async (n) => page.locator(`li.setting-row[data-setting="${n}"] .s-vis`).innerText();
      const tabTo = async (n) => page.locator('.settings-tabs').getByText(n, { exact: true }).click();
      check(`[${label}] プロフィール: 初期は自己紹介・学部・学科・学年がすべて非公開と表示される`, (await Promise.all(['自己紹介', '学部', '学科', '学年'].map(visOf))).every((t) => t.includes('非公開') && !t.includes('メンバーに公開')));
      check(`[${label}] プロフィールに横溢れがない`, (await overflowOf(page)) <= 0, `overflow=${await overflowOf(page)}`);
      const smallP = await smallOf(page);
      check(`[${label}] プロフィールの入力・ボタンの高さが44px以上`, smallP === 0, `44px未満=${smallP}`);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-profile.png`), fullPage: true });
      if (label === 'narrow') { await ctx.close(); continue; }

      // 変えたい項目だけ「変更」から編集する（ダイアログ。保存で完了のトースト）
      const edit = async (row, fill) => { await page.getByRole('button', { name: `${row}を変更` }).click(); const d = page.getByRole('dialog'); await fill(d); await d.getByRole('button', { name: '保存する' }).click(); };
      await edit('表示名', (d) => d.getByLabel('表示名（必須）').fill(pname));
      await page.locator('.ui-toast.success').filter({ hasText: '表示名を保存しました' }).waitFor();
      check(`[${label}] 表示名を変更でき、一覧の現在の内容に反映され、完了が表示される`, (await page.locator('li.setting-row[data-setting="表示名"] .s-value').innerText()).includes(pname));
      await edit('自己紹介', async (d) => { await d.getByLabel('自己紹介', { exact: true }).fill(`自己紹介です(${label})`); await d.getByLabel('公開範囲').selectOption('members'); });
      await page.locator('.ui-toast.success').filter({ hasText: '自己紹介を保存しました' }).waitFor();
      await edit('学部', (d) => d.getByLabel('学部', { exact: true }).fill('理工学部'));
      await page.locator('.ui-toast.success').filter({ hasText: '学部を保存しました' }).waitFor();
      await edit('学科', (d) => d.getByLabel('学科', { exact: true }).fill('情報科学科'));
      await page.locator('.ui-toast.success').filter({ hasText: '学科を保存しました' }).waitFor();
      await edit('学年', (d) => d.getByLabel('学年（1〜10）').fill('3'));
      await page.locator('.ui-toast.success').filter({ hasText: '学年を保存しました' }).waitFor();
      check(`[${label}] 変更していない項目の公開範囲は変わらない（自己紹介だけがメンバーに公開）`, (await visOf('自己紹介')).includes('メンバーに公開') && (await visOf('学部')).includes('非公開'));

      // 個人情報: 本人と管理者だけの注意書き。不正なメールはブラウザーの検証で止まり、入力は保持される
      await tabTo('個人情報');
      check(`[${label}] 個人情報は本人と管理者だけが見られる旨の注意書きがある`, /本人と、権限のある管理者（Admin・Owner）だけが見られます/.test(await page.locator('.notice-inline').innerText()));
      await edit('氏名', (d) => d.getByLabel('氏名', { exact: true }).fill(`秘密氏名${label}`));
      await page.locator('.ui-toast.success').filter({ hasText: '氏名を保存しました' }).waitFor();
      await page.getByRole('button', { name: '大学メールを変更' }).click();
      const md = page.getByRole('dialog');
      await md.getByLabel('大学メール').fill('not-an-email');
      await md.getByRole('button', { name: '保存する' }).click();
      check(`[${label}] 不正なメールはブラウザーの検証で止まり、ダイアログが開いたまま入力が保持される`, (await md.getByLabel('大学メール').evaluate((el) => !el.validity.valid)) && (await md.getByLabel('大学メール').inputValue()) === 'not-an-email');
      await md.getByLabel('大学メール').fill(`${label}@example.invalid`);
      await md.getByRole('button', { name: '保存する' }).click();
      await page.locator('.ui-toast.success').filter({ hasText: '大学メールを保存しました' }).waitFor();
      await edit('学生証番号', (d) => d.getByLabel('学生証番号').fill('99X-0001'));
      await page.locator('.ui-toast.success').filter({ hasText: '学生証番号を保存しました' }).waitFor();
      check(`[${label}] 個人情報の行は、現在の内容と「非公開」が表示される`, (await page.locator('li.setting-row[data-setting="氏名"]').innerText()).includes(`秘密氏名${label}`) && (await visOf('氏名')).includes('非公開'));

      // 外部リンク: 不正なURLはエラー、正しいURLは追加でき公開範囲が選べる
      await tabTo('プロフィール');
      const addLinkVia = async (name, url, vis) => {
        await page.getByRole('button', { name: 'リンクを追加する' }).click();
        const d = page.getByRole('dialog');
        await d.getByLabel('リンクの名前（例: GitHub）').fill(name);
        await d.getByLabel('URL', { exact: true }).fill(url);
        if (vis) await d.getByLabel('公開範囲').selectOption(vis);
        await d.getByRole('button', { name: '追加する' }).click();
      };
      await addLinkVia('悪いリンク', 'javascript:alert(1)');
      await page.locator('.notice.error').waitFor();
      check(`[${label}] javascript:のURLは登録できない`, /https:\/\/ または http:\/\/ で始まる/.test(await page.locator('.notice.error').innerText()));
      await addLinkVia('公開リンク', 'https://example.invalid/portfolio', 'members');
      await page.locator('li.setting-row[data-setting="公開リンク"]').waitFor();
      check(`[${label}] リンクを追加でき、公開範囲「メンバーに公開」が表示される`, (await visOf('公開リンク')).includes('メンバーに公開'));
      await page.getByRole('button', { name: '機材を追加する' }).click();
      const dd = page.getByRole('dialog');
      await dd.getByLabel('種類（例: PC、VR機器）').fill('PC');
      await dd.getByLabel('名前', { exact: true }).fill('自作PC');
      await dd.getByLabel('詳細（任意）').fill('高性能');
      await dd.getByLabel('公開範囲').selectOption('members');
      await dd.getByRole('button', { name: '追加する' }).click();
      await page.locator('li.setting-row[data-setting="自作PC"]').waitFor();
      check(`[${label}] 機材を追加でき、公開範囲が表示される`, (await visOf('自作PC')).includes('メンバーに公開'));

      // インターネット公開の同意は別（公開設定）。同意してもメンバー内公開の設定は変わらない
      await tabTo('公開設定');
      check(`[${label}] 公開設定に、どこに何が表示されるかの一覧がある`, (await page.locator('.vis-table tbody tr').count()) >= 4);
      check(`[${label}] 同意の前は「同意していません」`, /同意していません/.test(await page.locator('#consent-title ~ p').nth(1).innerText()));
      await page.getByRole('button', { name: 'インターネット公開に同意する' }).click();
      await page.locator('.ui-toast.success').filter({ hasText: '同意しました' }).waitFor();
      await tabTo('プロフィール');
      check(`[${label}] 同意しても、メンバー内公開の設定（自己紹介=メンバーに公開、学部=非公開）は変わらない`, (await visOf('自己紹介')).includes('メンバーに公開') && (await visOf('学部')).includes('非公開'));
      // 再読込しても保持される
      await page.reload();
      await page.getByRole('heading', { level: 1, name: 'アカウント設定' }).waitFor();
      await page.locator('li.setting-row[data-setting="表示名"]').waitFor();
      check(`[${label}] 再読込しても入力が保持される`, (await page.locator('li.setting-row[data-setting="表示名"] .s-value').innerText()).includes(pname) && (await visOf('自己紹介')).includes('メンバーに公開'));
      await tabTo('個人情報');
      check(`[${label}] 再読込しても個人情報が保持される`, (await page.locator('li.setting-row[data-setting="氏名"]').innerText()).includes(`秘密氏名${label}`));
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-profile-filled.png`), fullPage: true });
      await ctx.close();

      // 別のメンバー（Owner）から見る: 公開した項目だけが見える
      const vctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const vp = await vctx.newPage();
      await login(vp, o.ownerLogin, o.ownerPass);
      await vp.getByRole('link', { name: /^メンバー/ }).click();
      await vp.waitForURL('**/members');
      const mrow = vp.locator(`li.user[data-name="${pname}"]`);
      await mrow.waitFor();
      const mtext = await mrow.innerText();
      check(`[${label}] メンバー一覧に公開した自己紹介が出て、非公開の学部と個人情報は出ない`, mtext.includes(`自己紹介です(${label})`) && !mtext.includes('理工学部') && !mtext.includes(`秘密氏名${label}`));
      check(`[${label}] メンバー一覧に個人情報・ログインIDが一切出ない`, !(await vp.locator('body').innerText()).match(new RegExp(`秘密氏名|example\\.invalid|99X-0001|${pid.replace('.', '\\.')}`)));
      check(`[${label}] メンバー一覧に横溢れがない`, (await overflowOf(vp)) <= 0);
      if (o.shotsDir) await vp.screenshot({ path: join(o.shotsDir, `${label}-members.png`), fullPage: true });
      await vp.getByRole('searchbox').fill('プロフィール確認');
      await vp.getByRole('button', { name: '検索' }).click();
      await vp.waitForFunction((n) => document.querySelectorAll('li.user').length === n, 1).catch(() => undefined);
      await mrow.getByRole('link').click();
      await vp.waitForURL('**/members/*');
      await vp.getByRole('heading', { level: 1, name: pname }).waitFor();
      const dtext = await vp.locator('main').innerText();
      check(`[${label}] メンバー詳細: 公開した自己紹介・リンク・機材が見え、非公開の学部・個人情報は見えない`, dtext.includes('公開リンク') && dtext.includes('自作PC') && dtext.includes(`自己紹介です(${label})`) && !dtext.includes('理工学部') && !dtext.includes(`秘密氏名${label}`));
      const rel = await vp.getByRole('link', { name: '公開リンク' }).getAttribute('rel');
      check(`[${label}] 外部リンクは noopener noreferrer nofollow`, /noopener/.test(rel ?? '') && /noreferrer/.test(rel ?? '') && /nofollow/.test(rel ?? ''));

      // 管理者(Owner)が個人情報を見る → 停止 → 削除 → メンバー一覧から消える
      await vp.goto(adminUrl + '/admin/users');
      const urow = vp.locator(`li.user[data-login="${pid}"]`);
      await urow.waitFor();
      await urow.getByRole('button', { name: '個人情報を見る' }).click();
      const panel = urow.getByRole('region');
      await panel.waitFor();
      const ptext = await panel.innerText();
      check(`[${label}] 管理者は個人情報を見られ、閲覧が記録される旨が表示される`, ptext.includes(`秘密氏名${label}`) && ptext.includes('99X-0001') && /監査ログに記録/.test(ptext));
      check(`[${label}] 有効なユーザーには「個人情報を削除する」が出ない（先に停止）`, (await urow.getByRole('button', { name: '個人情報を削除する' }).count()) === 0);
      await panel.getByRole('button', { name: '閉じる' }).click();
      await urow.getByRole('button', { name: '停止する' }).click();
      await urow.getByRole('group').getByRole('button', { name: '停止する' }).click();
      await vp.waitForFunction((id) => /停止中/.test(document.querySelector(`li.user[data-login="${id}"]`)?.textContent ?? ''), pid);
      await urow.getByRole('button', { name: '個人情報を削除する' }).click();
      const form = urow.getByRole('form');
      await form.waitFor();
      check(`[${label}] 削除フォームに「元に戻せない」「本文や画像の本人情報は自動では消えない」の注意がある`, /元に戻せません/.test(await form.innerText()) && /自動では消えません/.test(await form.innerText()));
      check(`[${label}] 理由が空のあいだは削除ボタンが押せない`, await form.getByRole('button', { name: '完全に削除する' }).isDisabled());
      if (o.shotsDir) await vp.screenshot({ path: join(o.shotsDir, `${label}-delete-private.png`), fullPage: true });
      await form.getByLabel(/削除の理由/).fill('卒業のため本人から依頼(確認用)');
      await form.getByRole('button', { name: '完全に削除する' }).click();
      await vp.locator('.notice.success').filter({ hasText: '個人情報を削除しました' }).waitFor();
      await urow.getByRole('button', { name: '個人情報を見る' }).click();
      await urow.getByRole('region').waitFor();
      check(`[${label}] 削除後は個人情報が（未入力）になる`, !(await urow.getByRole('region').innerText()).includes(`秘密氏名${label}`) && (await urow.getByRole('region').innerText()).includes('（未入力）'));
      await vp.goto(adminUrl + '/members');
      await vp.locator('li.user').first().waitFor();
      check(`[${label}] 停止・削除したメンバーは現役のメンバー一覧に出ない`, (await vp.locator(`li.user[data-name="${pname}"]`).count()) === 0 && (await vp.locator('li.user[data-name="退会メンバー"]').count()) === 0);
      await vctx.close();
    }
    // 物品（一覧・詳細・貸出・代理/部分返却・貸出中・履歴。PC・スマホ・320px）
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
      const own = { cookie: o.ownerCookie, csrf: o.ownerCsrf };
      const mk = async (id, name) => {
        const c = await o.apiCall('POST', '/admin/users', { body: { loginId: id, displayName: name }, ...own });
        await o.apiCall('POST', '/auth/accept-invite', { body: { token: c.json.url.split('#token=')[1], password: 'ui-inventory-passphrase-1' } });
      };
      const aId = `ui.${label}.inva`, bId = `ui.${label}.invb`, aName = `借り手A(${label})`, bName = `借り手B(${label})`;
      await mk(aId, aName);
      await mk(bId, bName);
      const cable = `HDMIケーブル(${label})`, pico = `Pico(${label})`;
      const ci = await o.apiCall('POST', '/inventory/items', { body: { name: cable, category: 'ケーブル', location: '部室の棚', mode: 'quantity', totalQuantity: 5 }, ...own });
      const pi = await o.apiCall('POST', '/inventory/items', { body: { name: pico, category: 'マイコン', location: '部室の箱', mode: 'individual' }, ...own });
      await o.apiCall('POST', `/inventory/items/${pi.json.id}/units`, { body: { assetTag: `PICO-${label}-01` }, ...own });
      await o.apiCall('POST', `/inventory/items/${pi.json.id}/units`, { body: { assetTag: `PICO-${label}-02` }, ...own });

      const ctxA = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const pa = await ctxA.newPage();
      await login(pa, aId, 'ui-inventory-passphrase-1');
      await pa.getByRole('link', { name: /^物品/ }).click();
      await pa.waitForURL('**/inventory');
      await pa.locator(`a.item-card[data-name="${cable}"]`).waitFor();
      check(`[${label}] 物品一覧に物品が出て、残数が見える`, /残り 5 \/ 5/.test(await pa.locator(`a.item-card[data-name="${cable}"]`).innerText()));
      check(`[${label}] 物品一覧に横溢れがない`, (await overflowOf(pa)) <= 0, `overflow=${await overflowOf(pa)}`);
      check(`[${label}] 物品一覧の操作対象が44px以上`, (await smallOf(pa)) === 0, `small=${await smallOf(pa)}`);
      if (o.shotsDir) await pa.screenshot({ path: join(o.shotsDir, `${label}-inventory.png`), fullPage: true });
      await pa.getByRole('searchbox').fill('Pico'); // 入力が止まると自動で絞り込む
      await pa.locator(`a.item-card[data-name="${cable}"]`).waitFor({ state: 'detached' });
      await pa.locator(`a.item-card[data-name="${pico}"]`).waitFor();
      check(`[${label}] 検索で絞り込める`, (await pa.locator(`a.item-card[data-name="${cable}"]`).count()) === 0);
      await pa.getByRole('searchbox').fill('');
      await pa.locator(`a.item-card[data-name="${cable}"]`).waitFor();
      // 状態の表示: 全部貸出可能のあいだは「貸出可能」。カテゴリ・「借りられる物だけ」で絞れる
      check(`[${label}] 貸出可能な物品に「貸出可能」の札が出る`, /貸出可能/.test(await pa.locator(`a.item-card[data-name="${cable}"]`).innerText()));
      await pa.getByRole('button', { name: '借りられる物だけ' }).click();
      await pa.waitForTimeout(600);
      check(`[${label}] 「借りられる物だけ」で貸出可能な物品が残る`, (await pa.locator(`a.item-card[data-name="${cable}"]`).count()) === 1);
      await pa.getByRole('button', { name: '借りられる物だけ' }).click();
      await pa.locator(`a.item-card[data-name="${cable}"]`).waitFor();

      // 数量管理: 2個借りる → 残り3
      await pa.locator(`a.item-card[data-name="${cable}"]`).click();
      await pa.getByRole('heading', { level: 1, name: cable }).waitFor();
      await pa.getByLabel('借りる数', { exact: true }).fill('2');
      await pa.getByRole('button', { name: /^2個借りる$/ }).click();
      await pa.locator('.ui-toast.success').filter({ hasText: '残りは3個' }).waitFor();
      check(`[${label}] 借りた物品に「一部貸出中」と「自分が借りている」が出る`, /一部貸出中/.test(await pa.locator('main').innerText()) && /自分が借りている（2個）/.test(await pa.locator('main').innerText()));
      check(`[${label}] 2個借りると残りが3になる`, /残り 3/.test(await pa.locator('.stock').innerText()));
      check(`[${label}] 詳細に横溢れがない`, (await overflowOf(pa)) <= 0, `overflow=${await overflowOf(pa)}`);
      check(`[${label}] 詳細の操作対象が44px以上`, (await smallOf(pa)) === 0, `small=${await smallOf(pa)}`);
      if (o.shotsDir) await pa.screenshot({ path: join(o.shotsDir, `${label}-inventory-detail.png`), fullPage: true });

      // 別の人（B）が残り3個を借りて、残り0なら借りられない。Aの分を代理で一部返却する
      const ctxB = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const pb = await ctxB.newPage();
      await login(pb, bId, 'ui-inventory-passphrase-1');
      await pb.goto(`${adminUrl}/inventory/${ci.json.id}`);
      await pb.getByLabel('借りる数', { exact: true }).fill('3');
      await pb.getByRole('button', { name: /^3個借りる$/ }).click();
      await pb.locator('.ui-toast.success').filter({ hasText: '残りは0個' }).waitFor();
      check(`[${label}] 残り0になると借りるボタンが出ず、案内が出る`, (await pb.getByRole('button', { name: /個借りる$/ }).count()) === 0 && /借りられる数がありません/.test(await pb.locator('main').innerText()));
      const aRow = pb.locator('li.item').filter({ hasText: aName });
      await aRow.waitFor();
      await aRow.getByLabel(`${aName}さんの分の返却数`, { exact: true }).fill('1');
      await aRow.getByRole('button', { name: '返却する' }).click();
      await pb.locator('.ui-toast.success').filter({ hasText: '1個返却しました' }).waitFor();
      check(`[${label}] 他人の貸出を代理で一部返却でき、残りが1になる`, /残り 1/.test(await pb.locator('.stock').innerText()) && /1個/.test(await pb.locator('li.item').filter({ hasText: aName }).innerText()));

      // 個体管理: Aが個体1を借りる。同じ個体は他の人に借りるボタンが出ない
      await pa.goto(`${adminUrl}/inventory/${pi.json.id}`);
      const unit1 = pa.locator(`li.item[data-tag="PICO-${label}-01"]`);
      await unit1.getByRole('button', { name: 'この個体を借りる' }).click();
      await pa.locator('.ui-toast.success').filter({ hasText: `PICO-${label}-01 を借りました` }).waitFor();
      await pb.goto(`${adminUrl}/inventory/${pi.json.id}`);
      const u1b = pb.locator(`li.item[data-tag="PICO-${label}-01"]`);
      await u1b.waitFor();
      check(`[${label}] 貸出中の個体は借りた人が見え、借りるボタンが出ない`, (await u1b.getByRole('button').count()) === 0 && (await u1b.innerText()).includes(aName) && (await pb.locator(`li.item[data-tag="PICO-${label}-02"]`).getByRole('button', { name: 'この個体を借りる' }).count()) === 1);

      // 貸出中の一覧: 「自分だけ」と返却
      await pa.goto(`${adminUrl}/inventory/loans`);
      await pa.locator('li.item[data-loan]').first().waitFor();
      const allCount = await pa.locator('li.item[data-loan]').count();
      await pa.getByLabel('自分が借りているものだけ').check();
      await pa.locator('li.item[data-loan]').filter({ hasText: '（あなた）' }).first().waitFor();
      await pa.locator('li.item[data-loan]').filter({ hasText: bName }).first().waitFor({ state: 'detached' });
      const mineCount = await pa.locator('li.item[data-loan]').count();
      check(`[${label}] 貸出中の一覧で「自分だけ」に絞れ、経過日数が出る`, mineCount >= 1 && mineCount < allCount && /1日目|日目/.test(await pa.locator('li.item[data-loan]').first().innerText()), `all=${allCount} mine=${mineCount}`);
      check(`[${label}] 貸出中の一覧に横溢れがない`, (await overflowOf(pa)) <= 0, `overflow=${await overflowOf(pa)}`);
      check(`[${label}] 貸出中の一覧の操作対象が44px以上`, (await smallOf(pa)) === 0, `small=${await smallOf(pa)}`);
      if (o.shotsDir) await pa.screenshot({ path: join(o.shotsDir, `${label}-inventory-loans.png`), fullPage: true });
      await pa.locator('li.item[data-loan]').filter({ hasText: `PICO-${label}-01` }).getByRole('button', { name: '返却する' }).click();
      await pa.locator('.notice.success').filter({ hasText: '返却しました' }).waitFor();
      await pa.locator('li.item[data-loan]').filter({ hasText: `PICO-${label}-01` }).first().waitFor({ state: 'detached' });
      check(`[${label}] 貸出中の一覧から個体を返却すると一覧から消える`, (await pa.locator('li.item[data-loan]').filter({ hasText: `PICO-${label}-01` }).count()) === 0);

      // 履歴
      await pa.goto(`${adminUrl}/inventory/history`);
      await pa.locator('li.item[data-kind]').first().waitFor();
      const htext = await pa.locator('main').innerText();
      check(`[${label}] 履歴に貸出・返却（代理返却は登録者つき）が出る`, htext.includes(`${bName}さんが${cable}を3個借りました`) && htext.includes(`返却を登録した人: ${bName}さん`), htext.slice(0, 300));
      check(`[${label}] 履歴に横溢れがない`, (await overflowOf(pa)) <= 0, `overflow=${await overflowOf(pa)}`);
      if (o.shotsDir) await pa.screenshot({ path: join(o.shotsDir, `${label}-inventory-history.png`), fullPage: true });
      await ctxA.close();
      await ctxB.close();
    }
    // 物品の管理（Admin/Owner。登録・個体・総数変更・訂正。PC・スマホ・320px）
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
      const mpw = 'ui-inventory-passphrase-1';
      const mid = `ui.${label}.invm`, mname = `管理確認の借り手(${label})`;
      const c = await o.apiCall('POST', '/admin/users', { body: { loginId: mid, displayName: mname }, cookie: o.ownerCookie, csrf: o.ownerCsrf });
      await o.apiCall('POST', '/auth/accept-invite', { body: { token: c.json.url.split('#token=')[1], password: mpw } });
      const ml = await o.apiCall('POST', '/auth/login', { body: { loginId: mid, password: mpw } });
      const mem = { cookie: (ml.setCookie[0] ?? '').split(';')[0], csrf: ml.json.csrfToken };

      // 一般メンバーには「物品の管理」の導線が無く、登録しようとしてもAPIが拒否して案内が出る
      const mctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const mp = await mctx.newPage();
      await login(mp, mid, mpw);
      await mp.goto(`${adminUrl}/inventory`);
      await mp.getByRole('heading', { level: 1, name: '物品' }).waitFor();
      check(`[${label}] 一般メンバーの物品一覧に「物品の管理」の導線が出ない`, (await mp.getByRole('link', { name: '物品の管理' }).count()) === 0);
      await mp.goto(`${adminUrl}/admin/inventory`);
      await mp.getByLabel('名前（必須）').fill(`権限なし(${label})`);
      await mp.getByRole('button', { name: '登録する' }).click();
      await mp.locator('.notice.error').waitFor();
      check(`[${label}] 一般メンバーが物品を登録しようとするとAPIが拒否し、案内が出る`, /権限がありません/.test(await mp.locator('.notice.error').innerText()));
      await mctx.close();

      const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const page = await ctx.newPage();
      await login(page, o.ownerLogin, o.ownerPass);
      await page.goto(`${adminUrl}/inventory`);
      await page.getByRole('link', { name: '物品の管理' }).click();
      await page.waitForURL('**/admin/inventory');
      const qn = `管理ケーブル(${label})`, inn = `管理カメラ(${label})`;
      await page.getByLabel('名前（必須）').fill(qn);
      await page.getByLabel('カテゴリ').selectOption({ label: '機材' });
      await page.getByLabel('総数').fill('4');
      await page.getByRole('button', { name: '登録する' }).click();
      await page.locator('.ui-toast.success').filter({ hasText: `「${qn}」を登録しました` }).waitFor();
      await page.getByLabel('名前（必須）').fill(inn);
      await page.getByLabel('管理方式').selectOption('individual');
      check(`[${label}] 個体管理を選ぶと総数の入力が消える`, (await page.getByLabel('総数').count()) === 0);
      await page.getByRole('button', { name: '登録する' }).click();
      await page.locator('.ui-toast.success').filter({ hasText: `「${inn}」を登録しました` }).waitFor();
      check(`[${label}] 物品の管理に横溢れがない`, (await overflowOf(page)) <= 0, `overflow=${await overflowOf(page)}`);
      check(`[${label}] 物品の管理の操作対象が44px以上`, (await smallOf(page)) === 0, `small=${await smallOf(page)}`);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-inventory-admin.png`), fullPage: true });

      // 個体を登録する
      await page.locator(`li.item[data-name="${inn}"]`).getByRole('link', { name: '編集・個体' }).click();
      await page.getByRole('heading', { level: 1, name: inn }).waitFor();
      await page.getByLabel('管理番号（必須）').fill(`CAM-${label}-01`);
      await page.getByRole('button', { name: '個体を登録する' }).click();
      await page.locator(`li.item[data-tag="CAM-${label}-01"]`).waitFor();
      await page.getByLabel('管理番号（必須）').fill(`CAM-${label}-01`);
      await page.getByRole('button', { name: '個体を登録する' }).click();
      await page.locator('.notice.error').waitFor();
      check(`[${label}] 同じ管理番号の個体は登録できず、案内が出る`, (await page.locator(`li.item[data-tag="CAM-${label}-01"]`).count()) === 1 && (await page.locator('.notice.error').innerText()).length > 10);

      // 総数の変更: 理由が空だと押せない。貸出中より少ない総数は拒否される
      await page.goto(`${adminUrl}/admin/inventory`);
      await page.locator(`li.item[data-name="${qn}"]`).getByRole('link', { name: '編集・個体' }).click();
      await page.getByRole('heading', { level: 1, name: qn }).waitFor();
      const qid = page.url().split('/').pop();
      const lr = await o.apiCall('POST', '/inventory/loans', { body: { itemId: qid, quantity: 3 }, ...mem });
      check(`[${label}] （準備）一般メンバーが3個借りる`, lr.status === 201, `status=${lr.status} body=${JSON.stringify(lr.json)}`);
      await page.reload();
      await page.getByLabel('新しい総数').waitFor();
      await page.getByLabel('新しい総数').fill('2');
      check(`[${label}] 総数の変更は理由が空のあいだ押せない`, await page.getByRole('button', { name: '総数を変更する' }).isDisabled());
      await page.getByLabel('変更の理由（必須）').fill('確認用に減らす');
      await page.getByRole('button', { name: '総数を変更する' }).click();
      await page.locator('.notice.error').waitFor();
      check(`[${label}] 貸出中の数より少ない総数への変更は拒否される`, /現在の総数は4個/.test(await page.locator('main').innerText()) && (await page.locator('.notice.error').innerText()).length > 10);
      await page.getByLabel('新しい総数').fill('6');
      await page.getByRole('button', { name: '総数を変更する' }).click();
      await page.locator('.ui-toast.success').filter({ hasText: '総数を6個に変更しました' }).waitFor();
      await page.getByText('現在の総数は6個、貸出中は3個').waitFor({ timeout: 10000 }).catch(() => {});
      check(`[${label}] 総数を6に変更できる`, /現在の総数は6個、貸出中は3個/.test(await page.locator('main').innerText()));
      check(`[${label}] 物品の編集に横溢れがない・操作対象が44px以上`, (await overflowOf(page)) <= 0 && (await smallOf(page)) === 0, `overflow=${await overflowOf(page)} small=${await smallOf(page)}`);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-inventory-admin-edit.png`), fullPage: true });

      // 写真: 2枚を追加→並べ替え→保存。一覧のカードに先頭の写真が出る。1枚削除して保存し直せる
      const png = (n) => ({ name: `t${n}.png`, mimeType: 'image/png', buffer: makePng(64, 48, n) });
      await page.goto(`${adminUrl}/admin/inventory/${qid}`);
      await page.getByRole('heading', { level: 1, name: qn }).waitFor();
      await page.setInputFiles('#item-images-input', [png(1), png(2)]);
      check(`[${label}] 追加した写真が保存前にプレビューされ、「未保存」と出る`, (await page.locator('.image-tile').count()) === 2 && (await page.locator('.tile-new').count()) === 2);
      await page.getByRole('button', { name: '写真2を前へ' }).click();
      await page.getByRole('button', { name: '写真を保存する' }).click();
      await page.locator('.ui-toast.success').filter({ hasText: '写真を保存しました' }).waitFor();
      await page.locator('.image-tile').first().locator('.tile-main').waitFor();
      check(`[${label}] 写真を保存すると2枚が残り、先頭に「メイン」が付く`, (await page.locator('.image-tile').count()) === 2 && (await page.locator('.tile-new').count()) === 0 && (await page.locator('.tile-main').count()) === 1);
      const smallImg = await smallOf(page);
      check(`[${label}] 写真の編集の操作対象が44px以上・横溢れなし`, smallImg === 0 && (await overflowOf(page)) <= 0, `small=${smallImg} overflow=${await overflowOf(page)}`);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-inventory-admin-images.png`), fullPage: true });
      await page.getByRole('button', { name: '写真2をメインにする' }).or(page.getByRole('button', { name: 'メインにする' })).first().click();
      await page.getByRole('button', { name: '写真1を削除' }).click();
      await page.getByRole('button', { name: '写真を保存する' }).click();
      await page.locator('.ui-toast.success').filter({ hasText: '写真を保存しました' }).waitFor();
      await page.waitForFunction(() => document.querySelectorAll('.image-tile').length === 1);
      await page.goto(`${adminUrl}/inventory`);
      const card = page.locator(`a.item-card[data-name="${qn}"]`);
      await card.waitFor();
      await card.locator('img').waitFor();
      check(`[${label}] 物品一覧のカードに保存した写真が出る（画像が実際に読み込まれる）`, await card.locator('img').evaluate((img) => img.complete && img.naturalWidth > 0));

      // カテゴリの管理（Admin/Owner）: 追加→名前変更→削除。一般メンバーは追加できない
      const cn = `確認用カテゴリ(${label})`;
      await page.goto(`${adminUrl}/admin/inventory/categories`);
      await page.getByLabel('カテゴリ名').fill(cn);
      await page.getByRole('button', { name: '追加する' }).click();
      await page.locator(`li.item[data-name="${cn}"]`).waitFor();
      await page.locator(`li.item[data-name="${cn}"]`).getByRole('button', { name: '名前を変える' }).click();
      await page.getByLabel('新しい名前').fill(`${cn}改`);
      await page.getByRole('dialog').getByRole('button', { name: '保存する' }).click();
      await page.locator(`li.item[data-name="${cn}改"]`).waitFor();
      check(`[${label}] カテゴリを追加し、名前を変えられる`, (await page.locator(`li.item[data-name="${cn}"]`).count()) === 0);
      check(`[${label}] カテゴリの管理に横溢れがない・操作対象が44px以上`, (await overflowOf(page)) <= 0 && (await smallOf(page)) === 0, `overflow=${await overflowOf(page)} small=${await smallOf(page)}`);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-inventory-categories.png`), fullPage: true });
      await page.locator('li.item', { hasText: '機材' }).first().getByRole('button', { name: '削除' }).click();
      check(`[${label}] 使用中のカテゴリの削除では、移し先の選択が出る`, (await page.getByLabel('移し先のカテゴリ').count()) === 1);
      await page.getByRole('dialog').getByRole('button', { name: 'キャンセル' }).click();
      await page.locator(`li.item[data-name="${cn}改"]`).getByRole('button', { name: '削除' }).click();
      await page.getByRole('dialog').getByRole('button', { name: '削除する' }).click();
      await page.locator(`li.item[data-name="${cn}改"]`).waitFor({ state: 'detached' });
      check(`[${label}] 未使用のカテゴリを削除できる`, (await page.locator(`li.item[data-name="${cn}改"]`).count()) === 0);
      {
        const mctx3 = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
        const mp3 = await mctx3.newPage();
        await login(mp3, mid, mpw);
        await mp3.goto(`${adminUrl}/admin/inventory/categories`);
        await mp3.getByLabel('カテゴリ名').fill(`権限なし(${label})`);
        await mp3.getByRole('button', { name: '追加する' }).click();
        await mp3.locator('.notice.error').waitFor();
        check(`[${label}] 一般メンバーはカテゴリを追加できず、案内が出る`, /権限がありません/.test(await mp3.locator('.notice.error').innerText()));
        await mctx3.close();
      }

      // 訂正: 履歴から借り手の貸出を取り消す → 貸出中が0に戻り、元の記録と訂正が履歴に残る
      await page.goto(`${adminUrl}/inventory/history`);
      const ev = page.locator('li.item[data-kind="loan"]').filter({ hasText: qn }).filter({ hasText: mname });
      await ev.waitFor();
      await ev.getByRole('button', { name: 'この記録を訂正する' }).click();
      const form = ev.getByRole('form', { name: '記録の訂正' });
      check(`[${label}] 訂正は理由が空のあいだ押せず、元の記録が残る注意がある`, (await form.getByRole('button', { name: '訂正する' }).isDisabled()) && /元の記録は消えず/.test(await form.innerText()));
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-inventory-correction.png`), fullPage: true });
      await form.getByLabel('訂正の理由（必須）').fill('誤って登録されたため(確認用)');
      await form.getByRole('button', { name: '訂正する' }).click();
      await page.locator('.notice.success').filter({ hasText: '記録を取り消しました' }).waitFor();
      await page.locator('li.item[data-kind="correction"]').filter({ hasText: qn }).first().waitFor();
      const htext = await page.locator('main').innerText();
      check(`[${label}] 取消後も元の貸出が履歴に残り、訂正（理由つき）が追加される`, htext.includes(`${mname}さんが${qn}を3個借りました`) && htext.includes('理由: 誤って登録されたため(確認用)'));
      await page.goto(`${adminUrl}/inventory/${qid}`);
      await page.locator('.stock').waitFor();
      check(`[${label}] 取消後は残りが総数（6）に戻る`, /残り 6 \/ 全?6|残り 6/.test(await page.locator('.stock').innerText()), await page.locator('.stock').innerText());
      // 一般メンバーには訂正の導線が出ない
      const mctx2 = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const mp2 = await mctx2.newPage();
      await login(mp2, mid, mpw);
      await mp2.goto(`${adminUrl}/inventory/history`);
      await mp2.locator('li.item[data-kind]').first().waitFor();
      const mtext = await mp2.locator('main').innerText();
      check(`[${label}] 一般メンバーの履歴に訂正の導線と訂正の理由が出ない`, (await mp2.getByRole('button', { name: 'この記録を訂正する' }).count()) === 0 && !mtext.includes('誤って登録されたため'));
      await mctx2.close();

      // 廃止: 貸出が無ければ廃止でき、一覧から消える
      await page.goto(`${adminUrl}/admin/inventory/${qid}`);
      await page.getByRole('heading', { level: 1, name: qn }).waitFor();
      page.once('dialog', (d) => d.accept());
      await page.getByRole('button', { name: '廃止する' }).click();
      await page.waitForURL('**/admin/inventory');
      await page.locator(`li.item[data-name="${inn}"]`).waitFor();
      check(`[${label}] 廃止した物品は管理の一覧から消える`, (await page.locator(`li.item[data-name="${qn}"]`).count()) === 0);
      await ctx.close();
    }
    // 通知の設定（Admin/Owner。PC・スマホ・320px）
    for (const [label, width, height, ch] of [['pc', 1440, 900, '111111111111111111'], ['sp', 390, 844, '222222222222222222'], ['narrow', 320, 640, '333333333333333333']]) {
      const own = { cookie: o.ownerCookie, csrf: o.ownerCsrf };
      // 毎回同じ状態から始める（全種別を有効・チャンネル未設定）
      for (const et of ['inventory.loan_created', 'inventory.loan_returned', 'inventory.correction_created']) {
        await o.apiCall('PUT', `/admin/notifications/settings/${et}`, { body: { enabled: true, discordChannelId: '' }, ...own });
      }
      // 一般メンバーには設定が見えず、権限が無い案内が出る
      const mctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const mp = await mctx.newPage();
      await login(mp, o.memberLogin, o.memberPass);
      await mp.goto(`${adminUrl}/admin/notifications`);
      await mp.locator('.notice.error').waitFor();
      check(`[${label}] 一般メンバーには通知の設定が出ず、権限が無い案内が出る`, /権限がありません/.test(await mp.locator('.notice.error').innerText()) && (await mp.locator('li.setting').count()) === 0);
      await mctx.close();

      const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const page = await ctx.newPage();
      await login(page, o.ownerLogin, o.ownerPass);
      await page.goto(`${adminUrl}/admin/inventory`);
      await page.getByRole('link', { name: '通知の設定' }).click();
      await page.waitForURL('**/admin/notifications');
      await page.locator('li.setting').first().waitFor();
      check(`[${label}] 通知の設定に4種別が出る`, (await page.locator('li.setting').count()) === 4);
      const row = page.locator('li.setting[data-event="inventory.loan_created"]');
      check(`[${label}] 変更が無いあいだは「保存する」が押せない`, await row.getByRole('button', { name: '保存する' }).isDisabled());
      check(`[${label}] チャンネル未設定の案内が出る`, /送信待ちとして溜まります/.test(await row.innerText()));
      // 数字以外のIDはAPIが拒否し、案内が出る
      await row.getByLabel('送信先のチャンネルID').fill('abc');
      await row.getByRole('button', { name: '保存する' }).click();
      await page.locator('.notice.error').waitFor();
      check(`[${label}] 数字以外のチャンネルIDは保存できず、案内が出る`, /数字だけ/.test(await page.locator('.notice.error').innerText()));
      await row.getByLabel('送信先のチャンネルID').fill(ch);
      await row.getByRole('button', { name: '保存する' }).click();
      await page.locator('.notice.success').filter({ hasText: '設定を保存しました' }).waitFor();
      check(`[${label}] 保存すると「保存する」が押せなくなる`, await row.getByRole('button', { name: '保存する' }).isDisabled());
      // Botが無い環境の接続テストは、設定が必要な旨を案内する
      await row.getByRole('button', { name: '接続テスト' }).click();
      await page.locator('.notice.error').filter({ hasText: 'Botが設定されていません' }).waitFor();
      check(`[${label}] Bot未設定の接続テストは案内が出る（送信はされない）`, true);
      // OFFにして保存 → 再読み込みしても保持される
      const row2 = page.locator('li.setting[data-event="inventory.loan_returned"]');
      await row2.getByLabel('この通知を送る').uncheck();
      await row2.getByRole('button', { name: '保存する' }).click();
      await page.locator('.notice.success').filter({ hasText: '設定を保存しました' }).waitFor();
      await page.reload();
      await page.locator('li.setting').first().waitFor();
      check(`[${label}] 設定が再読み込み後も保持される`, (await page.locator('li.setting[data-event="inventory.loan_created"]').getByLabel('送信先のチャンネルID').inputValue()) === ch && !(await page.locator('li.setting[data-event="inventory.loan_returned"]').getByLabel('この通知を送る').isChecked()));
      check(`[${label}] 通知の設定に横溢れがない・操作対象が44px以上`, (await overflowOf(page)) <= 0 && (await smallOf(page)) === 0, `overflow=${await overflowOf(page)} small=${await smallOf(page)}`);

      // 送信の状況: 送信待ち（貸出の通知が溜まっている）と、止まった通知の再送
      if (o.dbExec) {
        o.dbExec(`INSERT INTO notification_outbox (event_type, dedupe_key, payload_json, status, attempts, last_error) VALUES ('inventory.loan_created', 'e2e-dead-${label}', '{"v":1,"itemName":"確認用"}', 'dead', 8, 'Discordのサーバーエラー（HTTP 503）（再試行の上限）')`);
        await page.reload();
        await page.locator('li.setting').first().waitFor();
        await page.getByLabel('表示する状態').selectOption('dead');
        const dead = page.locator(`li.item[data-delivery][data-status="dead"]`).filter({ hasText: 'HTTP 503' });
        await dead.first().waitFor();
        check(`[${label}] 失敗して停止した通知に、失敗の要約と「再送する」が出る（本文は出ない）`, (await dead.first().getByRole('button', { name: '再送する' }).count()) === 1 && !(await page.locator('main').innerText()).includes('確認用'));
        if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-notifications.png`), fullPage: true });
        await dead.first().getByRole('button', { name: '再送する' }).click();
        await page.locator('.notice.success').filter({ hasText: '再送待ちに戻しました' }).waitFor();
        await page.locator('li.item[data-delivery][data-status="dead"]').filter({ hasText: 'HTTP 503' }).first().waitFor({ state: 'detached' });
        await page.getByLabel('表示する状態').selectOption('pending');
        await page.locator('li.item[data-delivery][data-status="pending"]').first().waitFor();
        check(`[${label}] 再送すると停止の一覧から消え、送信待ちに戻る`, (await page.locator('li.item[data-delivery][data-status="pending"]').count()) >= 1);
      }
      await ctx.close();
    }
    // 既存記事の投稿者（D14。Admin/Owner。PC・スマホ・320px）。架空の名前だけを使う
    if (o.dbExec) {
      for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
        const nm = `架空投稿者${label}`;
        o.dbExec(`DELETE FROM legacy_authors WHERE source_name='${nm}'`);
        o.dbExec(`INSERT INTO legacy_authors (source_name) VALUES ('${nm}')`);
        const mctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
        const mp = await mctx.newPage();
        await login(mp, o.memberLogin, o.memberPass);
        await mp.goto(`${adminUrl}/admin/legacy-authors`);
        await mp.locator('.notice.error').waitFor();
        check(`[${label}] 一般メンバーには投稿者の一覧が出ない`, (await mp.locator('li.item[data-author]').count()) === 0);
        await mctx.close();
        const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
        const page = await ctx.newPage();
        await login(page, o.ownerLogin, o.ownerPass);
        await page.goto(`${adminUrl}/admin/legacy-authors`);
        const row = page.locator(`li.item[data-author="${nm}"]`);
        await row.waitFor();
        check(`[${label}] 未確認の投稿者は「nu_digital」と表示される案内が出る`, /未確認/.test(await row.innerText()));
        const btn = row.getByRole('button', { name: '本人の許可を確認した（公開する）' });
        check(`[${label}] 確認の記録が空のあいだは公開できない`, await btn.isDisabled());
        await row.getByLabel(/本人の許可をどう確認したか/).fill('本人に口頭で確認');
        await btn.click();
        await page.locator('.notice.success').filter({ hasText: '公開の許可を記録しました' }).waitFor();
        check(`[${label}] 許可を記録すると「公開を許可」になる`, /公開を許可/.test(await row.innerText()));
        check(`[${label}] 投稿者の画面で横スクロールが出ない`, (await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)) <= 0);
        if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-legacy-authors.png`), fullPage: true });
        await row.getByRole('button', { name: '公開を止める' }).click();
        await page.locator('.notice.success').filter({ hasText: '公開を止めました' }).waitFor();
        o.dbExec(`DELETE FROM legacy_authors WHERE source_name='${nm}'`);
        await ctx.close();
      }
    }
    // アバター・物品の写真（P3-02b）: 画像を選ぶ→設定→表示→外す（PC・スマホ・320px）
    {
      // 40x30の単色PNG（APIが再エンコードする）。zlibは Node 組み込み
      const { deflateSync, crc32 } = await import('node:zlib');
      const chunk = (t, d) => { const l = Buffer.alloc(4); l.writeUInt32BE(d.length); const c = Buffer.alloc(4); c.writeUInt32BE(crc32(Buffer.concat([Buffer.from(t), d])) >>> 0); return Buffer.concat([l, Buffer.from(t), d, c]); };
      const mkPng = (w, h, rgb) => {
        const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2;
        const raw = Buffer.alloc((w * 3 + 1) * h);
        for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) { const o = y * (w * 3 + 1) + 1 + x * 3; raw[o] = rgb[0]; raw[o + 1] = rgb[1] + ((x * 5) % 40); raw[o + 2] = rgb[2]; }
        return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
      };
      for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
        const own = { cookie: o.ownerCookie, csrf: o.ownerCsrf };
        const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
        const page = await ctx.newPage();
        await login(page, o.ownerLogin, o.ownerPass);
        await page.goto(`${adminUrl}/profile`);
        await page.getByRole('button', { name: 'アイコンを変更' }).click();
        const adlg = page.getByRole('dialog');
        await adlg.locator('#avatar-file').setInputFiles({ name: 'avatar.png', mimeType: 'image/png', buffer: mkPng(40, 30, [200, 60, 60]) });
        await adlg.locator('canvas').waitFor();
        await adlg.getByRole('button', { name: '保存する' }).click();
        await page.locator('.ui-toast.success').filter({ hasText: 'プロフィール画像を変更しました' }).waitFor();
        const img = page.locator('li.setting-row[data-setting="アイコン"] .ui-avatar img');
        await img.waitFor();
        check(`[${label}] アイコンをトリミングして設定すると画像が表示される（WebPとして読める）`, await img.evaluate((i) => new Promise((r) => { if (i.complete) r(i.naturalWidth > 0); else i.onload = () => r(i.naturalWidth > 0); })), '');
        check(`[${label}] アバターの画面で横スクロールが出ない`, (await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)) <= 0);
        if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-avatar.png`), fullPage: true });
        await page.goto(`${adminUrl}/members`);
        await page.locator('li.user img.avatar').first().waitFor();
        check(`[${label}] メンバー一覧にアバターが出る`, (await page.locator('li.user img.avatar').count()) >= 1);
        await page.goto(`${adminUrl}/profile`);
        await page.getByRole('button', { name: 'アイコンを変更' }).click();
        await page.getByRole('dialog').getByRole('button', { name: '画像を外す' }).click();
        await page.locator('.ui-toast.success').filter({ hasText: 'プロフィール画像を外しました' }).waitFor();
        check(`[${label}] アイコンを外すと画像が消える`, (await page.locator('li.setting-row[data-setting="アイコン"] .ui-avatar img').count()) === 0);
        // 物品の写真（管理者）
        const it = await o.apiCall('POST', '/inventory/items', { body: { name: `写真確認用(${label})`, mode: 'quantity', totalQuantity: 2 }, ...own });
        await page.goto(`${adminUrl}/admin/inventory/${it.json.id}`);
        await page.locator('#item-images-input').setInputFiles({ name: 'item.png', mimeType: 'image/png', buffer: mkPng(60, 40, [40, 120, 200]) });
        await page.getByRole('button', { name: '写真を保存する' }).click();
        await page.locator('.ui-toast.success').filter({ hasText: '写真を保存しました' }).waitFor();
        await page.goto(`${adminUrl}/inventory/${it.json.id}`);
        const photo = page.locator('.gallery-main img');
        await photo.waitFor();
        check(`[${label}] 物品の写真が詳細に表示される`, await photo.evaluate((i) => i.complete && i.naturalWidth > 0));
        await page.goto(`${adminUrl}/inventory`);
        await page.locator('.item-card .thumb img').first().waitFor();
        check(`[${label}] 物品の一覧にサムネイルが出る`, (await page.locator('.item-card .thumb img').count()) >= 1);
        check(`[${label}] 物品の画面で横スクロールが出ない`, (await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)) <= 0);
        if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-item-photo.png`), fullPage: true });
        await ctx.close();
      }
    }
    // 公開版に残る本人情報の確認（D08。Admin/Owner。PC・スマホ・320px）。結果は架空の語だけ
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
      const ctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const page = await ctx.newPage();
      await login(page, o.ownerLogin, o.ownerPass);
      await page.goto(`${adminUrl}/admin/published-search`);
      await page.getByLabel('探す語（2〜50文字）').fill('ZzNoSuchWordZz');
      await page.getByRole('button', { name: '探す' }).click();
      await page.getByTestId('no-hits').waitFor();
      check(`[${label}] 見つからないときは、その旨が出る`, /見つかりませんでした/.test(await page.getByTestId('no-hits').innerText()));
      check(`[${label}] 公開版の確認の画面で横スクロールが出ない`, (await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)) <= 0);
      const mctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const mp = await mctx.newPage();
      await login(mp, o.memberLogin, o.memberPass);
      await mp.goto(`${adminUrl}/admin/published-search`);
      await mp.getByLabel('探す語（2〜50文字）').fill('abcdef');
      await mp.getByRole('button', { name: '探す' }).click();
      await mp.locator('.notice.error').waitFor();
      check(`[${label}] 一般メンバーは探せない（権限の案内が出る）`, /権限/.test(await mp.locator('.notice.error').innerText()));
      await mctx.close();
      await ctx.close();
    }
    // 記事・作品（CMS）: 下書き・プレビュー（描画時のサニタイズ）・保存の競合・公開（PC・スマホ・320px）
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
      const own = { cookie: o.ownerCookie, csrf: o.ownerCsrf };
      const pw = 'ui-cms-passphrase-1';
      const mk = async (id, name, role) => {
        const c = await o.apiCall('POST', '/admin/users', { body: { loginId: id, displayName: name }, ...own });
        await o.apiCall('POST', '/auth/accept-invite', { body: { token: c.json.url.split('#token=')[1], password: pw } });
        if (role) { const rr = await o.apiCall('PUT', `/admin/users/${c.json.user.id}/editor-role`, { body: { enabled: true }, ...own }); if (rr.status !== 200) throw new Error(`編集者の役割を付けられない: ${rr.status}`); }
      };
      const wid = `ui.${label}.cmsw`, eid = `ui.${label}.cmse`, xid = `ui.${label}.cmsx`;
      await mk(wid, `書き手(${label})`);
      await mk(eid, `編集者(${label})`, true);
      await mk(xid, `無関係(${label})`);
      const slug = `ui-cms-${label}`;
      const title = `画面の確認の記事(${label})`;

      const wctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const wp = await wctx.newPage();
      await wp.route('https://www.youtube-nocookie.com/**', (r) => r.abort());
      await login(wp, wid, pw);
      await wp.getByRole('link', { name: /^記事・作品/ }).click();
      await wp.waitForURL('**/contents');
      await wp.getByRole('link', { name: '新しく書く' }).click();
      await wp.waitForURL('**/contents/new');
      await wp.getByLabel('タイトル（必須）').fill(title);
      await wp.getByLabel('URL用の名前（必須）').fill(slug);
      await wp.getByRole('button', { name: '下書きを作る' }).click();
      await wp.getByRole('heading', { level: 1, name: title }).waitFor();
      const url = wp.url();
      const cid = url.split('/').pop();
      check(`[${label}] 下書きを作ると編集画面に移り、保存ボタンは変更があるまで押せない`, await wp.getByRole('button', { name: '保存する', exact: true }).isDisabled());
      check(`[${label}] 一般メンバーには公開ボタンが無く、公開できるのは編集者以上と案内される`, (await wp.getByRole('button', { name: /を公開する/ }).count()) === 0 && /公開できるのは編集者/.test(await wp.locator('main').innerText()));

      // 本文を書いて保存 → 版が増える。保存後は保存ボタンが押せない
      const tab = async (name) => { if (width < 1100) await wp.locator('.only-narrow').getByText(name, { exact: true }).click(); };
      await wp.getByLabel('本文（Markdown）').fill('## 見出しです\n\n本文の**強調**と、動画:\n\n<div data-youtube="ZEguS-om_Ec"></div>\n');
      check(`[${label}] 変更があると保存ボタンが押せて、「保存していない変更」と出る`, !(await wp.getByRole('button', { name: '保存する', exact: true }).isDisabled()) && /保存していない変更があります/.test(await wp.locator('main').innerText()));
      await tab('プレビュー');
      await wp.locator('[data-testid="preview"] h2').filter({ hasText: '見出しです' }).waitFor();
      check(`[${label}] プレビューに見出し・強調・YouTubeの埋め込み（youtube-nocookie）が出る`, (await wp.locator('[data-testid="preview"] strong').count()) === 1 && (await wp.locator('[data-testid="preview"] iframe[src^="https://www.youtube-nocookie.com/embed/ZEguS-om_Ec"]').count()) === 1);
      if (o.shotsDir) await wp.screenshot({ path: join(o.shotsDir, `${label}-cms-preview.png`), fullPage: true });
      await tab('編集');
      await wp.getByRole('button', { name: '保存する', exact: true }).click();
      await wp.locator('.ui-toast.success').filter({ hasText: '保存しました（版2）' }).waitFor();
      check(`[${label}] 保存すると版2になり、保存ボタンが押せなくなる`, await wp.getByRole('button', { name: '保存する', exact: true }).isDisabled());

      // 危険な記述は、保存前でもプレビューの段階で取り除かれ（描画時のサニタイズ）、保存はAPIが拒否する
      await wp.evaluate(() => { window.__xss = undefined; });
      const evil = '<img src=x onerror="window.__xss=1">\n\n<script>window.__xss=2</script>\n\n[リンク](javascript:window.__xss=3)\n\n<iframe src="https://evil.example/"></iframe>\n\n<a href="javascript:window.__xss=4" onclick="window.__xss=5">x</a>\n';
      await wp.getByLabel('本文（Markdown）').fill(evil);
      await tab('プレビュー');
      await wp.locator('[data-testid="preview"]').waitFor();
      await wp.waitForTimeout(300);
      const pv = await wp.locator('[data-testid="preview"]').innerHTML();
      check(`[${label}] プレビューは、script・iframe・イベント属性・javascript:を表示の段階で取り除く（保存前の入力でも）`, !/<script|<iframe|onerror|onclick|javascript:/i.test(pv) && (await wp.evaluate(() => window.__xss)) === undefined, pv.slice(0, 160));
      await tab('編集');
      await wp.getByRole('button', { name: '保存する', exact: true }).click();
      await wp.locator('.notice.error').filter({ hasText: '本文に使えない記述があります' }).waitFor();
      check(`[${label}] 危険な記述の保存はAPIが422で拒否し、入力は保持される`, (await wp.getByLabel('本文（Markdown）').inputValue()) === evil);
      await wp.getByLabel('本文（Markdown）').fill('## 見出しです\n\n本文の**強調**と、動画:\n\n<div data-youtube="ZEguS-om_Ec"></div>\n');
      check(`[${label}] 編集画面に横溢れがない・操作対象が44px以上`, (await overflowOf(wp)) <= 0 && (await smallOf(wp)) === 0, `overflow=${await overflowOf(wp)} small=${await smallOf(wp)}`);
      if (o.shotsDir) await wp.screenshot({ path: join(o.shotsDir, `${label}-cms-editor.png`), fullPage: true });

      // 保存の競合: 編集者が先に保存 → 書き手の保存は競合になり、最新を読み込める
      const ectx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const ep = await ectx.newPage();
      await login(ep, eid, pw);
      await ep.goto(`${adminUrl}/contents/${cid}`);
      await ep.getByRole('heading', { level: 1, name: title }).waitFor();
      await ep.getByLabel('タイトル（必須）').fill(`${title}・編集者が直した`);
      await ep.getByRole('button', { name: '保存する', exact: true }).click();
      await ep.locator('.ui-toast.success').filter({ hasText: '保存しました（版3）' }).waitFor();
      await wp.getByLabel('タイトル（必須）').fill(`${title}・書き手の変更`);
      await wp.getByRole('button', { name: '保存する', exact: true }).click();
      await wp.getByRole('region', { name: '保存の競合' }).waitFor();
      check(`[${label}] 他の人が先に保存していると、競合の案内と2つの選択肢が出る（保存は成立しない）`, (await wp.getByRole('button', { name: /最新を読み込む/ }).count()) === 1 && (await wp.getByRole('button', { name: /最新の上に保存し直す/ }).count()) === 1 && /編集者\(/.test(await wp.locator('.notice.error').innerText()));
      await wp.getByRole('button', { name: /最新を読み込む/ }).click();
      await wp.waitForFunction((t) => document.querySelector('#e-title')?.value === t, `${title}・編集者が直した`);
      check(`[${label}] 「最新を読み込む」で、編集者の保存した内容になる`, (await wp.getByLabel('タイトル（必須）').inputValue()) === `${title}・編集者が直した`);

      // 公開: 編集者だけ。公開すると公開APIに出る。公開後の編集は再公開まで反映されない
      await ep.reload();
      await ep.getByRole('button', { name: '版3を公開する' }).first().waitFor();
      await ep.getByRole('button', { name: '版3を公開する' }).first().click();
      await ep.getByRole('dialog').getByRole('button', { name: '公開する' }).click();
      await ep.locator('.ui-toast.success').filter({ hasText: '版3を公開しました' }).waitFor();
      const pubGet = () => ectx.request.get(`${adminUrl}/api/v1/public/contents/blog/${slug}`);
      let pr = await pubGet();
      check(`[${label}] 公開すると、匿名で読める公開APIに出る`, pr.status() === 200 && (await pr.json()).title === `${title}・編集者が直した`);
      await wp.getByLabel('タイトル（必須）').fill(`${title}・公開後の編集`);
      await wp.getByRole('button', { name: '保存する', exact: true }).click();
      await wp.locator('.ui-toast.success').filter({ hasText: '保存しました（版4）' }).waitFor();
      pr = await pubGet();
      check(`[${label}] 公開後に書き手が保存しても、再公開まで公開APIは旧版のまま`, (await pr.json()).title === `${title}・編集者が直した`);
      await wp.goto(`${adminUrl}/contents`);
      await wp.locator(`li[data-slug="${slug}"]`).waitFor();
      const liText = await wp.locator(`li[data-slug="${slug}"]`).innerText();
      check(`[${label}] 一覧に「公開中」と「公開後の変更あり（未公開）」が出る`, /公開中/.test(liText) && /未公開の変更あり/.test(liText));
      check(`[${label}] 記事・作品の一覧に横溢れがない・操作対象が44px以上`, (await overflowOf(wp)) <= 0 && (await smallOf(wp)) === 0, `overflow=${await overflowOf(wp)} small=${await smallOf(wp)}`);
      if (o.shotsDir) await wp.screenshot({ path: join(o.shotsDir, `${label}-cms-list.png`), fullPage: true });
      await ep.reload();
      await ep.getByRole('button', { name: '公開を止める' }).first().click();
      await ep.getByRole('dialog').getByRole('button', { name: '公開を止める' }).click();
      await ep.locator('.ui-toast.success').filter({ hasText: '公開を止めました' }).waitFor();
      pr = await pubGet();
      check(`[${label}] 公開を止めると、公開APIは404になる`, pr.status() === 404);

      // 無関係なメンバーには、下書きの編集画面が開かない（存在を漏らさない）
      const xctx = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const xp = await xctx.newPage();
      await login(xp, xid, pw);
      await xp.goto(`${adminUrl}/contents/${cid}`);
      await xp.locator('.notice.error').waitFor();
      check(`[${label}] 無関係なメンバーには、下書きの編集画面が開かず本文も出ない`, /見つからない/.test(await xp.locator('.notice.error').innerText()) && !(await xp.locator('body').innerText()).includes('公開後の編集'));
      await xp.goto(`${adminUrl}/contents`);
      await xp.getByRole('heading', { level: 1, name: '記事・作品' }).waitFor();
      await xp.waitForTimeout(300);
      check(`[${label}] 無関係なメンバーの一覧に、他人の下書きが出ない`, (await xp.locator(`li[data-slug="${slug}"]`).count()) === 0);
      await wctx.close(); await ectx.close(); await xctx.close();
    }
    // 記事・作品（CMS）その2: 画像・共同編集者・制作メンバーと公開同意による匿名化（PC・スマホ・320px）
    for (const [label, width, height] of [['pc', 1440, 900], ['sp', 390, 844], ['narrow', 320, 640]]) {
      const { randomBytes } = await import('node:crypto');
      const zlib = await import('node:zlib');
      const chunk = (type, data) => {
        const len = Buffer.alloc(4); len.writeUInt32BE(data.length);
        const crc = Buffer.alloc(4); crc.writeUInt32BE(zlib.crc32(Buffer.concat([Buffer.from(type), data])) >>> 0);
        return Buffer.concat([len, Buffer.from(type), data, crc]);
      };
      const png = (w, h) => {
        const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2;
        const raw = Buffer.alloc((w * 3 + 1) * h);
        for (let y = 0; y < h; y++) randomBytes(w * 3).copy(raw, y * (w * 3 + 1) + 1);
        return Buffer.concat([Buffer.from('89504e470d0a1a0a', 'hex'), chunk('IHDR', ihdr), chunk('IDAT', zlib.deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
      };
      const own = { cookie: o.ownerCookie, csrf: o.ownerCsrf };
      const pw = 'ui-cms2-passphrase-1';
      const ids = {};
      const mk = async (key, name, role) => {
        const loginId = `ui.${label}.c2${key}`;
        const c = await o.apiCall('POST', '/admin/users', { body: { loginId, displayName: name }, ...own });
        await o.apiCall('POST', '/auth/accept-invite', { body: { token: c.json.url.split('#token=')[1], password: pw } });
        if (role) await o.apiCall('PUT', `/admin/users/${c.json.user.id}/editor-role`, { body: { enabled: true }, ...own });
        const x = await o.apiCall('POST', '/auth/login', { body: { loginId, password: pw } });
        ids[key] = { loginId, name, userId: c.json.user.id, cookie: (x.setCookie[0] ?? '').split(';')[0], csrf: x.json.csrfToken };
      };
      await mk('w', `執筆者2(${label})`);
      await mk('e', `編集者2(${label})`, true);
      await mk('k', `共同候補(${label})`);
      await mk('a', `制作者A(${label})`);
      await mk('b', `制作者B(${label})`);
      const asUser = (u) => ({ cookie: u.cookie, csrf: u.csrf });
      const mkContent = async (body) => (await o.apiCall('POST', '/contents', { body, ...asUser(ids.w) })).json.id;
      const newCtx = async (u) => { const c = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' }); const p = await c.newPage(); await login(p, u.loginId, pw); return [c, p]; };
      const anon = (path) => fetch(`${adminUrl}${path}`);

      // --- 画像 ---
      const aid = await mkContent({ type: 'article', section: 'blog', slug: `ui-img-${label}`, title: `画像の記事(${label})`, markdown: '' });
      const [wc, wp] = await newCtx(ids.w);
      const [ec, ep] = await newCtx(ids.e);
      await wp.goto(`${adminUrl}/contents/${aid}`);
      await wp.getByRole('heading', { level: 1, name: `画像の記事(${label})` }).waitFor();
      await wp.locator('#e-file').setInputFiles({ name: 'fake.png', mimeType: 'image/png', buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>') });
      await wp.locator('.notice.error').filter({ hasText: 'JPEG・PNG・WebP' }).waitFor();
      check(`[${label}] 画像でないファイル（SVG）は、拒否の案内が出て、アップロード一覧に出ない`, (await wp.locator('[data-testid="uploads"]').count()) === 0);
      await wp.locator('#e-file').setInputFiles({ name: 'photo.png', mimeType: 'image/png', buffer: png(40, 30) });
      await wp.locator('.ui-toast.success').filter({ hasText: '画像をアップロードしました（40×30）' }).waitFor();
      await wp.getByRole('button', { name: '本文に挿入' }).click();
      const mdVal = await wp.getByLabel('本文（Markdown）').inputValue();
      const inlineUrl = (mdVal.match(/\(\/api\/v1\/public\/media\/([A-Za-z0-9_-]{22})\)/) ?? [])[1];
      check(`[${label}] アップロードした画像を本文に挿入できる（画像のURLが入る）`, !!inlineUrl);
      await wp.getByRole('button', { name: 'カバーにする' }).click();
      await wp.locator('[data-testid="cover"] img').waitFor();
      const cover = await wp.locator('[data-testid="cover"] img').evaluate((el) => new Promise((r) => { if (el.complete) r(el.naturalWidth); else el.onload = () => r(el.naturalWidth); }));
      check(`[${label}] カバー画像が（認証つきの経路で）表示される`, cover === 40, `naturalWidth=${cover}`);
      if (width < 1100) await wp.locator('.only-narrow').getByText('プレビュー', { exact: true }).click();
      await wp.locator('[data-testid="preview"] img').first().waitFor();
      const pv = await wp.locator('[data-testid="preview"] img').first().evaluate((el) => new Promise((r) => { const done = () => r({ w: el.naturalWidth, src: el.getAttribute('src') }); if (el.complete) done(); else el.onload = done; }));
      check(`[${label}] プレビューに、下書きの画像が認証つきの経路で表示される（公開URLのままでは匿名に出ない）`, pv.w === 40 && pv.src === `/api/v1/media/by-public/${inlineUrl}`, JSON.stringify(pv));
      if (width < 1100) await wp.locator('.only-narrow').getByText('編集', { exact: true }).click();
      check(`[${label}] 画像の欄に横溢れがない・操作対象が44px以上`, (await overflowOf(wp)) <= 0 && (await smallOf(wp)) === 0, `overflow=${await overflowOf(wp)} small=${await smallOf(wp)}`);
      if (o.shotsDir) await wp.screenshot({ path: join(o.shotsDir, `${label}-cms-images.png`), fullPage: true });
      await wp.getByRole('button', { name: '保存する', exact: true }).click();
      await wp.locator('.ui-toast.success').filter({ hasText: '保存しました' }).waitFor();
      await wp.reload();
      await wp.locator('[data-testid="cover"] img').waitFor();
      check(`[${label}] 保存して読み込み直しても、カバー画像と本文の画像が保たれる`, (await wp.getByLabel('本文（Markdown）').inputValue()).includes(inlineUrl));
      check(`[${label}] 公開前は、画像は匿名に配信されない`, (await anon(`/api/v1/public/media/${inlineUrl}`)).status === 404);
      await ep.goto(`${adminUrl}/contents/${aid}`);
      await ep.getByRole('button', { name: /を公開する/ }).first().waitFor();
      await ep.getByRole('button', { name: /を公開する/ }).first().click();
      await ep.getByRole('dialog').getByRole('button', { name: '公開する' }).click();
      await ep.locator('.ui-toast.success').filter({ hasText: '公開しました' }).waitFor();
      const detail = await (await anon(`/api/v1/public/contents/blog/ui-img-${label}`)).json();
      const coverId = detail.coverUrl?.split('/').pop();
      check(`[${label}] 公開後は、本文の画像とカバー画像が匿名に配信される（公開APIにはカバーのURLだけが出る）`, (await anon(`/api/v1/public/media/${inlineUrl}`)).status === 200 && !!coverId && (await anon(detail.coverUrl)).status === 200 && !JSON.stringify(detail).includes('"id"'));
      await ep.reload();
      await ep.getByRole('button', { name: '公開を止める' }).click();
      await ep.getByRole('dialog').getByRole('button', { name: '公開を止める' }).click();
      await ep.locator('.ui-toast.success').filter({ hasText: '公開を止めました' }).waitFor();
      check(`[${label}] 公開を止めると、画像もすぐ匿名に配信されなくなる`, (await anon(`/api/v1/public/media/${inlineUrl}`)).status === 404);

      // --- 共同編集者 ---
      await wp.getByLabel('メンバーを追加する').fill(`共同候補(${label})`);
      await wp.getByRole('button', { name: `共同候補(${label})さんを追加` }).click();
      await wp.getByRole('button', { name: 'メンバーを保存する' }).click();
      await wp.locator('.ui-toast.success').filter({ hasText: 'メンバーを保存しました' }).waitFor();
      check(`[${label}] メンバーを追加すると一覧に出る（作者＋追加した人）`, (await wp.locator('[data-testid="members"] [data-person]').count()) === 2);
      const [kc, kp] = await newCtx(ids.k);
      await kp.goto(`${adminUrl}/contents/${aid}`);
      await kp.getByRole('heading', { level: 1, name: `画像の記事(${label})` }).waitFor();
      check(`[${label}] 共同編集者は編集画面を開けるが、共同編集者の変更欄と公開ボタンは出ない`, (await kp.getByLabel('メンバーを追加する').count()) === 0 && (await kp.getByRole('button', { name: /を公開する/ }).count()) === 0 && /追加・削除ができるのは、作者と編集者/.test(await kp.locator('main').innerText()));
      await kp.getByLabel('タイトル（必須）').fill(`画像の記事(${label})・共同編集`);
      await kp.getByRole('button', { name: '保存する', exact: true }).click();
      await kp.locator('.ui-toast.success').filter({ hasText: '保存しました' }).waitFor();
      check(`[${label}] 共同編集者は本文を保存できる`, true);
      await wp.getByRole('button', { name: `共同候補(${label})さんを外す` }).click();
      await wp.getByRole('button', { name: 'メンバーを保存する' }).click();
      await wp.locator('.ui-toast.success').filter({ hasText: 'メンバーを保存しました' }).waitFor();
      await kp.reload();
      await kp.locator('.notice.error').waitFor();
      check(`[${label}] 外された共同編集者には、編集画面が開かない`, /見つからない/.test(await kp.locator('.notice.error').innerText()));
      await kc.close();

      // --- 制作メンバーと公開同意による匿名化 ---
      const pid = await mkContent({ type: 'project', section: 'project', slug: `ui-work-${label}`, title: `作品の記事(${label})`, markdown: '作品の本文' });
      await wp.goto(`${adminUrl}/contents/${pid}`);
      await wp.getByRole('heading', { level: 1, name: `作品の記事(${label})` }).waitFor();
      check(`[${label}] 作品の編集画面にメンバーの欄があり、公開同意の説明が出る`, /名前が出るのは、本人が「インターネットへの公開」に同意している場合だけ/.test(await wp.locator('#members-title').locator('xpath=..').innerText()) && (await wp.getByRole('button', { name: 'メンバーを保存する' }).isDisabled()));
      for (const [key, role] of [['a', '企画'], ['b', '音楽']]) {
        await wp.getByLabel('メンバーを追加する').fill(ids[key].name);
        await wp.getByRole('button', { name: `${ids[key].name}さんを追加` }).click();
        await wp.getByLabel(`${ids[key].name}さんの役割`).fill(role);
      }
      await wp.getByRole('button', { name: 'メンバーを保存する' }).click();
      await wp.locator('.ui-toast.success').filter({ hasText: 'メンバーを保存しました' }).waitFor();
      check(`[${label}] 制作メンバーを保存すると新しい版（版2）になり、作者と2人が並ぶ`, (await wp.locator('[data-testid="members"] [data-person]').count()) === 3 && /版2/.test(await wp.locator('main').innerText()));
      if (o.shotsDir) await wp.screenshot({ path: join(o.shotsDir, `${label}-cms-contributors.png`), fullPage: true });
      await ep.goto(`${adminUrl}/contents/${pid}`);
      await ep.getByRole('button', { name: '版2を公開する' }).first().waitFor();
      await ep.getByRole('button', { name: '版2を公開する' }).first().click();
      await ep.getByRole('dialog').getByRole('button', { name: '公開する' }).click();
      await ep.locator('.ui-toast.success').filter({ hasText: '版2を公開しました' }).waitFor();
      const credits = async () => (await (await anon(`/api/v1/public/contents/project/ui-work-${label}`)).json()).contributors.map((c) => `${c.displayName}:${c.roleLabel}`).join(',');
      check(`[${label}] 同意していない制作メンバーは、公開サイトで「匿名のメンバー」と出る（役割は出る）`, (await credits()) === '匿名のメンバー:企画,匿名のメンバー:音楽', await credits());
      await o.apiCall('PUT', '/me/profile/public-credit-consent', { body: { granted: true }, ...asUser(ids.a) });
      check(`[${label}] 本人が公開に同意すると、再公開しなくても公開サイトに名前が出る（同意していない人は匿名のまま）`, (await credits()) === `${ids.a.name}:企画,匿名のメンバー:音楽`, await credits());
      // プロフィールの制作実績: 本人には常に、他のメンバーには同意が続く間だけ
      const [ac, ap] = await newCtx(ids.a);
      await ap.goto(`${adminUrl}/profile`);
      await ap.locator('[data-testid="works"] li').first().waitFor();
      check(`[${label}] 本人のプロフィールに、公開された作品が制作実績として出る（役割つき）`, /作品の記事/.test(await ap.locator('[data-testid="works"]').innerText()) && /企画/.test(await ap.locator('[data-testid="works"]').innerText()));
      await wp.goto(`${adminUrl}/members/${ids.a.userId}`);
      await wp.locator('[data-testid="works"] li').first().waitFor();
      check(`[${label}] 同意が続いている間は、他のメンバーにも制作実績が見える`, /作品の記事/.test(await wp.locator('[data-testid="works"]').innerText()));
      await o.apiCall('PUT', '/me/profile/public-credit-consent', { body: { granted: false }, ...asUser(ids.a) });
      check(`[${label}] 同意を取り消すと、公開サイトの名前はすぐ匿名に戻る`, (await credits()) === '匿名のメンバー:企画,匿名のメンバー:音楽', await credits());
      await wp.reload();
      await wp.getByRole('heading', { level: 1 }).waitFor();
      check(`[${label}] 同意を取り消すと、他のメンバーからは制作実績が見えなくなる（本人には見える）`, (await wp.locator('[data-testid="works"]').count()) === 0);
      await ap.reload();
      await ap.locator('[data-testid="works"] li').first().waitFor();
      check(`[${label}] 制作実績の表示に横溢れがない`, (await overflowOf(ap)) <= 0, `overflow=${await overflowOf(ap)}`);
      await ac.close(); await wc.close(); await ec.close();
    }
    // 権限が無い人（一般メンバー）にはAPIが拒否し、画面は案内を表示する
    {
      const ctx = await browser.newContext({ viewport: { width: 390, height: 844 } });
      const page = await ctx.newPage();
      await login(page, o.memberLogin, o.memberPass);
      await page.getByRole('link', { name: /ユーザー管理/ }).click();
      await page.locator('.notice.error').waitFor();
      check('一般メンバーにはユーザー管理の権限が無い案内が出て、一覧は出ない', /権限がありません/.test(await page.locator('.notice.error').innerText()) && (await page.locator('li.user').count()) === 0);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, 'sp-users-forbidden.png') });
      await ctx.close();
    }
    // トークン無しで開いた場合の案内
    const ctx3 = await browser.newContext();
    const p3 = await ctx3.newPage();
    await p3.goto(`${adminUrl}/reset`);
    await p3.locator('.notice.error').waitFor();
    check('リンクが正しくない場合の案内が出る', /リンクが正しくありません/.test(await p3.locator('.notice.error').innerText()));
    const res = await ctx3.request.get(`${adminUrl}/invite`);
    check('招待画面は no-referrer・no-store', res.headers()['referrer-policy'] === 'no-referrer' && /no-store/.test(res.headers()['cache-control'] ?? ''), JSON.stringify({ rp: res.headers()['referrer-policy'], cc: res.headers()['cache-control'] }));
    await ctx3.close();
  } finally {
    await browser.close();
  }
}
