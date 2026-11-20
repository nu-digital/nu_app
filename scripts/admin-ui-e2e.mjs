// 内部アプリ(apps/admin)の画面E2E。実ブラウザー(Playwright)で、ログイン・ランチャー・招待受諾・再設定を操作する。
// compose-smoke.mjs から呼ばれる（起動済みのスタックに対して実行）。単体では実行しない。
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import { join, resolve } from 'node:path';

const require = createRequire(join(resolve('apps/admin'), 'x.js'));
const { chromium } = require('playwright');

/**
 * @param {{adminUrl:string, apiCall:Function, ownerCookie:string, ownerCsrf:string, memberLogin:string, memberPass:string, memberName:string, shotsDir?:string, check:(name:string, ok:boolean, detail?:string)=>void}} o
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
      const toggleH = await page.locator('.toggle').first().evaluate((el) => el.getBoundingClientRect().height);
      check(`[${label}] 「表示する」ボタンが折り返さない`, toggleH <= 60, `height=${toggleH}`);
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
      check(`[${label}] 全員共通のカードが4枚あり、未実装は「準備中」`, (await page.locator('.cards li').count()) === 4 && (await page.getByText('準備中').count()) === 4);
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
