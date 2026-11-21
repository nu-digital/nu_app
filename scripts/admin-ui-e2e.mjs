// 内部アプリ(apps/admin)の画面E2E。実ブラウザー(Playwright)で、ログイン・ランチャー・招待受諾・再設定を操作する。
// compose-smoke.mjs から呼ばれる（起動済みのスタックに対して実行）。単体では実行しない。
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import { join, resolve } from 'node:path';

const require = createRequire(join(resolve('apps/admin'), 'x.js'));
const { chromium } = require('playwright');

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
      await page.getByRole('heading', { level: 1, name: 'プロフィール' }).waitFor();
      check(`[${label}] プロフィール: 初期はすべて非公開で、個人情報は本人と管理者だけの注意書きがある`, (await page.locator('select[id^="vis-"]').evaluateAll((els) => els.every((e) => e.value === 'private'))) && /本人と、権限のある管理者（Admin・Owner）だけが見られます/.test(await page.locator('.notice-inline').innerText()));
      check(`[${label}] プロフィールに横溢れがない`, (await overflowOf(page)) <= 0, `overflow=${await overflowOf(page)}`);
      const smallP = await smallOf(page);
      check(`[${label}] プロフィールの入力・ボタンの高さが44px以上`, smallP === 0, `44px未満=${smallP}`);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-profile.png`), fullPage: true });
      if (label === 'narrow') { await ctx.close(); continue; }

      const note = async () => page.locator('.notice').first();
      await page.getByLabel('表示名（必須）').fill(pname);
      await page.getByLabel('自己紹介', { exact: true }).fill(`自己紹介です(${label})`);
      await page.getByLabel('学部', { exact: true }).fill('理工学部');
      await page.getByLabel('学科', { exact: true }).fill('情報科学科');
      await page.getByLabel('学年（1〜10）').fill('3');
      await page.locator('#vis-bio').selectOption('members');
      await page.getByRole('button', { name: '基本情報を保存する' }).click();
      await page.locator('.notice.success').waitFor();
      check(`[${label}] 基本情報を保存でき、完了が表示される`, /基本情報を保存しました/.test(await page.locator('.notice.success').innerText()));
      await page.getByLabel('氏名', { exact: true }).fill(`秘密氏名${label}`);
      await page.getByLabel('大学メール').fill('not-an-email');
      await page.getByRole('button', { name: '個人情報を保存する' }).click();
      // type=email のため、ブラウザーが形式エラーを伝えて送信を止める（入力は保持される）
      check(`[${label}] 不正なメールはブラウザーの検証で止まり、入力が保持される`, (await page.getByLabel('大学メール').evaluate((el) => !el.validity.valid)) && (await page.getByLabel('氏名', { exact: true }).inputValue()) === `秘密氏名${label}`);
      await page.getByLabel('大学メール').fill(`${label}@example.invalid`);
      await page.getByLabel('学生証番号').fill('99X-0001');
      await page.getByRole('button', { name: '個人情報を保存する' }).click();
      await page.locator('.notice.success').waitFor();
      // 外部リンク: 不正なURLはエラー、正しいURLは追加でき公開範囲を変えられる
      await page.getByLabel('リンクの名前').fill('悪いリンク');
      await page.getByLabel('URL', { exact: true }).fill('javascript:alert(1)');
      await page.getByRole('button', { name: 'リンクを追加する' }).click();
      await page.locator('.notice.error').waitFor();
      check(`[${label}] javascript:のURLは登録できない`, /https:\/\/ または http:\/\/ で始まる/.test(await page.locator('.notice.error').innerText()));
      await page.getByLabel('リンクの名前').fill('公開リンク');
      await page.getByLabel('URL', { exact: true }).fill('https://example.invalid/portfolio');
      await page.getByRole('button', { name: 'リンクを追加する' }).click();
      await page.locator('li.item[data-label="公開リンク"]').waitFor();
      await page.getByLabel('公開リンクの公開範囲').selectOption('members');
      await page.locator('.notice.success').filter({ hasText: '公開範囲を変更しました' }).waitFor();
      await page.getByLabel('種類（例: PC、VR機器）').fill('PC');
      await page.getByLabel('名前', { exact: true }).fill('自作PC');
      await page.getByLabel('詳細（任意）').fill('高性能');
      await page.getByRole('button', { name: '機材を追加する' }).click();
      await page.locator('li.item[data-name="自作PC"]').waitFor();
      await page.getByLabel('自作PCの公開範囲').selectOption('members'); // 機材の公開範囲も初期は非公開。公開する場合は本人が選ぶ
      await page.locator('.notice.success').filter({ hasText: '公開範囲を変更しました' }).waitFor();
      // インターネット公開の同意は別: 同意してもメンバー内公開の設定は変わらない
      check(`[${label}] 同意の前は「同意していません」`, /同意していません/.test(await page.locator('#consent-title ~ p').nth(1).innerText()));
      await page.getByRole('button', { name: 'インターネット公開に同意する' }).click();
      await page.locator('.notice.success').filter({ hasText: '同意しました' }).waitFor();
      check(`[${label}] 同意しても、メンバー内公開の設定（自己紹介=メンバーに公開、学部=非公開）は変わらない`, (await page.locator('#vis-bio').inputValue()) === 'members' && (await page.locator('#vis-faculty').inputValue()) === 'private');
      // 再読込しても保持される
      await page.reload();
      await page.getByLabel('表示名（必須）').waitFor();
      check(`[${label}] 再読込しても入力が保持される`, (await page.getByLabel('表示名（必須）').inputValue()) === pname && (await page.getByLabel('氏名', { exact: true }).inputValue()) === `秘密氏名${label}` && (await page.locator('#vis-bio').inputValue()) === 'members');
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
      await pa.getByLabel('物品を探す').fill('Pico');
      await pa.getByRole('button', { name: '絞り込む' }).click();
      await pa.locator(`a.item-card[data-name="${pico}"]`).waitFor();
      check(`[${label}] 検索で絞り込める`, (await pa.locator(`a.item-card[data-name="${cable}"]`).count()) === 0);
      await pa.getByLabel('物品を探す').fill('');
      await pa.getByRole('button', { name: '絞り込む' }).click();
      await pa.locator(`a.item-card[data-name="${cable}"]`).waitFor();

      // 数量管理: 2個借りる → 残り3
      await pa.locator(`a.item-card[data-name="${cable}"]`).click();
      await pa.getByRole('heading', { level: 1, name: cable }).waitFor();
      await pa.getByLabel('借りる数').fill('2');
      await pa.getByRole('button', { name: '借りる', exact: true }).click();
      await pa.locator('.notice.success').filter({ hasText: '残りは3個' }).waitFor();
      check(`[${label}] 2個借りると残りが3になる`, /残り 3/.test(await pa.locator('.stock').innerText()));
      check(`[${label}] 詳細に横溢れがない`, (await overflowOf(pa)) <= 0, `overflow=${await overflowOf(pa)}`);
      check(`[${label}] 詳細の操作対象が44px以上`, (await smallOf(pa)) === 0, `small=${await smallOf(pa)}`);
      if (o.shotsDir) await pa.screenshot({ path: join(o.shotsDir, `${label}-inventory-detail.png`), fullPage: true });

      // 別の人（B）が残り3個を借りて、残り0なら借りられない。Aの分を代理で一部返却する
      const ctxB = await browser.newContext({ viewport: { width, height }, locale: 'ja-JP', reducedMotion: 'reduce' });
      const pb = await ctxB.newPage();
      await login(pb, bId, 'ui-inventory-passphrase-1');
      await pb.goto(`${adminUrl}/inventory/${ci.json.id}`);
      await pb.getByLabel('借りる数').fill('3');
      await pb.getByRole('button', { name: '借りる', exact: true }).click();
      await pb.locator('.notice.success').filter({ hasText: '残りは0個' }).waitFor();
      check(`[${label}] 残り0になると借りるボタンが出ず、案内が出る`, (await pb.getByRole('button', { name: '借りる', exact: true }).count()) === 0 && /借りられる数がありません/.test(await pb.locator('main').innerText()));
      const aRow = pb.locator('li.item').filter({ hasText: aName });
      await aRow.waitFor();
      await aRow.getByLabel(/返却数/).fill('1');
      await aRow.getByRole('button', { name: '返却する' }).click();
      await pb.locator('.notice.success').filter({ hasText: '1個返却しました' }).waitFor();
      check(`[${label}] 他人の貸出を代理で一部返却でき、残りが1になる`, /残り 1/.test(await pb.locator('.stock').innerText()) && /1個/.test(await pb.locator('li.item').filter({ hasText: aName }).innerText()));

      // 個体管理: Aが個体1を借りる。同じ個体は他の人に借りるボタンが出ない
      await pa.goto(`${adminUrl}/inventory/${pi.json.id}`);
      const unit1 = pa.locator(`li.item[data-tag="PICO-${label}-01"]`);
      await unit1.getByRole('button', { name: 'この個体を借りる' }).click();
      await pa.locator('.notice.success').filter({ hasText: `PICO-${label}-01 を借りました` }).waitFor();
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
      await page.getByLabel('カテゴリ').fill('ケーブル');
      await page.getByLabel('総数').fill('4');
      await page.getByRole('button', { name: '登録する' }).click();
      await page.locator('.notice.success').filter({ hasText: `「${qn}」を登録しました` }).waitFor();
      await page.getByLabel('名前（必須）').fill(inn);
      await page.getByLabel('管理方式').selectOption('individual');
      check(`[${label}] 個体管理を選ぶと総数の入力が消える`, (await page.getByLabel('総数').count()) === 0);
      await page.getByRole('button', { name: '登録する' }).click();
      await page.locator('.notice.success').filter({ hasText: `「${inn}」を登録しました` }).waitFor();
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
      await page.locator('.notice.success').filter({ hasText: '総数を6個に変更しました' }).waitFor();
      await page.getByText('現在の総数は6個、貸出中は3個').waitFor({ timeout: 10000 }).catch(() => {});
      check(`[${label}] 総数を6に変更できる`, /現在の総数は6個、貸出中は3個/.test(await page.locator('main').innerText()));
      check(`[${label}] 物品の編集に横溢れがない・操作対象が44px以上`, (await overflowOf(page)) <= 0 && (await smallOf(page)) === 0, `overflow=${await overflowOf(page)} small=${await smallOf(page)}`);
      if (o.shotsDir) await page.screenshot({ path: join(o.shotsDir, `${label}-inventory-admin-edit.png`), fullPage: true });

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
      check(`[${label}] 下書きを作ると編集画面に移り、保存ボタンは変更があるまで押せない`, await wp.getByRole('button', { name: '保存する' }).isDisabled());
      check(`[${label}] 一般メンバーには公開ボタンが無く、公開できるのは編集者以上と案内される`, (await wp.getByRole('button', { name: /を公開する/ }).count()) === 0 && /公開できるのは編集者/.test(await wp.locator('main').innerText()));

      // 本文を書いて保存 → 版が増える。保存後は保存ボタンが押せない
      const tab = async (name) => { if (width < 1100) await wp.getByRole('tab', { name }).click(); };
      await wp.getByLabel('本文（Markdown）').fill('## 見出しです\n\n本文の**強調**と、動画:\n\n<div data-youtube="ZEguS-om_Ec"></div>\n');
      check(`[${label}] 変更があると保存ボタンが押せて、「保存していない変更」と出る`, !(await wp.getByRole('button', { name: '保存する' }).isDisabled()) && /保存していない変更があります/.test(await wp.locator('main').innerText()));
      await tab('プレビュー');
      await wp.locator('[data-testid="preview"] h2').filter({ hasText: '見出しです' }).waitFor();
      check(`[${label}] プレビューに見出し・強調・YouTubeの埋め込み（youtube-nocookie）が出る`, (await wp.locator('[data-testid="preview"] strong').count()) === 1 && (await wp.locator('[data-testid="preview"] iframe[src^="https://www.youtube-nocookie.com/embed/ZEguS-om_Ec"]').count()) === 1);
      if (o.shotsDir) await wp.screenshot({ path: join(o.shotsDir, `${label}-cms-preview.png`), fullPage: true });
      await tab('編集');
      await wp.getByRole('button', { name: '保存する' }).click();
      await wp.locator('.notice.success').filter({ hasText: '保存しました（版2）' }).waitFor();
      check(`[${label}] 保存すると版2になり、保存ボタンが押せなくなる`, await wp.getByRole('button', { name: '保存する' }).isDisabled());

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
      await wp.getByRole('button', { name: '保存する' }).click();
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
      await ep.getByRole('button', { name: '保存する' }).click();
      await ep.locator('.notice.success').filter({ hasText: '保存しました（版3）' }).waitFor();
      await wp.getByLabel('タイトル（必須）').fill(`${title}・書き手の変更`);
      await wp.getByRole('button', { name: '保存する' }).click();
      await wp.getByRole('region', { name: '保存の競合' }).waitFor();
      check(`[${label}] 他の人が先に保存していると、競合の案内と2つの選択肢が出る（保存は成立しない）`, (await wp.getByRole('button', { name: /最新を読み込む/ }).count()) === 1 && (await wp.getByRole('button', { name: /最新の上に保存し直す/ }).count()) === 1 && /編集者\(/.test(await wp.locator('.notice.error').innerText()));
      await wp.getByRole('button', { name: /最新を読み込む/ }).click();
      await wp.waitForFunction((t) => document.querySelector('#e-title')?.value === t, `${title}・編集者が直した`);
      check(`[${label}] 「最新を読み込む」で、編集者の保存した内容になる`, (await wp.getByLabel('タイトル（必須）').inputValue()) === `${title}・編集者が直した`);

      // 公開: 編集者だけ。公開すると公開APIに出る。公開後の編集は再公開まで反映されない
      await ep.reload();
      await ep.getByRole('button', { name: '版3を公開する' }).waitFor();
      ep.once('dialog', (d) => d.accept());
      await ep.getByRole('button', { name: '版3を公開する' }).click();
      await ep.locator('.notice.success').filter({ hasText: '版3を公開しました' }).waitFor();
      const pubGet = () => ectx.request.get(`${adminUrl}/api/v1/public/contents/blog/${slug}`);
      let pr = await pubGet();
      check(`[${label}] 公開すると、匿名で読める公開APIに出る`, pr.status() === 200 && (await pr.json()).title === `${title}・編集者が直した`);
      await wp.getByLabel('タイトル（必須）').fill(`${title}・公開後の編集`);
      await wp.getByRole('button', { name: '保存する' }).click();
      await wp.locator('.notice.success').filter({ hasText: '保存しました（版4）' }).waitFor();
      pr = await pubGet();
      check(`[${label}] 公開後に書き手が保存しても、再公開まで公開APIは旧版のまま`, (await pr.json()).title === `${title}・編集者が直した`);
      await wp.goto(`${adminUrl}/contents`);
      await wp.locator(`li.item[data-slug="${slug}"]`).waitFor();
      const liText = await wp.locator(`li.item[data-slug="${slug}"]`).innerText();
      check(`[${label}] 一覧に「公開中」と「公開後の変更あり（未公開）」が出る`, /公開中/.test(liText) && /公開後の変更あり/.test(liText));
      check(`[${label}] 記事・作品の一覧に横溢れがない・操作対象が44px以上`, (await overflowOf(wp)) <= 0 && (await smallOf(wp)) === 0, `overflow=${await overflowOf(wp)} small=${await smallOf(wp)}`);
      if (o.shotsDir) await wp.screenshot({ path: join(o.shotsDir, `${label}-cms-list.png`), fullPage: true });
      await ep.reload();
      ep.once('dialog', (d) => d.accept());
      await ep.getByRole('button', { name: '公開を止める' }).click();
      await ep.locator('.notice.success').filter({ hasText: '公開を止めました' }).waitFor();
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
      check(`[${label}] 無関係なメンバーの一覧に、他人の下書きが出ない`, (await xp.locator(`li.item[data-slug="${slug}"]`).count()) === 0);
      await wctx.close(); await ectx.close(); await xctx.close();
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
