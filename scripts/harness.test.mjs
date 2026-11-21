import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync, spawnSync } from 'node:child_process';
import { policyErrors, forbiddenPath, linkErrors, repositoryErrors, root, siteContentPattern, workflowPolicyErrors } from './check.mjs';
import { stopResult } from './claude-stop.mjs';

test('Issue番号付きブランチと日本語タイトルを受け入れる', () => {
  assert.deepEqual(policyErrors({ head: { ref: 'feat/12-invite-link' }, title: 'feat(auth): 招待を検証する' }), []);
  assert.deepEqual(policyErrors({ head: { ref: 'chore/bootstrap-agent-harness' }, title: 'chore: 実装の準備を整える' }), []);
});

test('main・不正なIssue番号・英語だけのタイトルを拒否する', () => {
  for (const ref of ['main', 'feat/no-issue', 'feat/0-test', 'feat/12-UPPER', 'chore/bootstrap-other']) {
    assert.equal(policyErrors({ head: { ref }, title: 'feat: 実装する' }).length, 1);
  }
  assert.equal(policyErrors({ head: { ref: 'fix/1-bug' }, title: 'fix: update' }).length, 1);
  assert.equal(policyErrors({ head: { ref: 'fix/1-bug' }, title: '修正した' }).length, 1);
});

test('Dependabot例外は実際の作成者と専用ブランチの両方が必要', () => {
  const pr = { head: { ref: 'dependabot/github_actions/update' }, title: 'Bump action' };
  assert.deepEqual(policyErrors({ ...pr, user: { login: 'dependabot[bot]' } }), []);
  assert.equal(policyErrors({ ...pr, user: { login: 'someone' } }).length, 2);
  assert.deepEqual(policyErrors(undefined), []);
});

test('secret等の禁止ファイルと架空envテンプレートを区別する', () => {
  for (const file of ['.env', 'apps/admin/.env.local', 'deploy/key.pem', 'backup.sql.gz', '.local/file.md', '.claude/settings.local.json', 'CLAUDE.local.md']) {
    assert.equal(forbiddenPath(file), true, file);
  }
  for (const file of ['.env.example', 'apps/admin/.env.test.example', 'db/migrations/001.sql', 'CLAUDE.md']) {
    assert.equal(forbiddenPath(file), false, file);
  }
});

test('リンク切れ・リポジトリ外リンクを検出し、URLとコード例は除外', () => {
  const fixture = mkdtempSync(join(tmpdir(), 'nu-harness-test-'));
  try {
    mkdirSync(join(fixture, 'docs'));
    writeFileSync(join(fixture, 'README.md'), '# fixture');
    assert.deepEqual(linkErrors('docs/test.md', '[ok](../README.md) [web](https://example.invalid) [anchor](#section)\n```md\n[sample](missing)\n```', fixture), []);
    assert.equal(linkErrors('docs/test.md', '[missing](missing.md)', fixture).length, 1);
    assert.equal(linkErrors('docs/test.md', '[outside](../../outside.md)', fixture).length, 1);
    assert.equal(linkErrors('docs/test.md', '[invalid](%zz)', fixture).length, 1);
  } finally {
    // Only the exact directory created by mkdtemp above is removed.
    rmSync(fixture, { recursive: true, force: true });
  }
});

test('Stop hookは失敗時にブロックし、成功時にはブロックしない', () => {
  assert.equal(stopResult({}, () => false).decision, 'block');
  assert.equal(stopResult({}, () => true), null);
});

test('Stop hookによる再開時は繰り返しブロックしない', () => {
  assert.equal(stopResult({ stop_hook_active: true }, () => { throw new Error('実行されない'); }), null);
});

test('実リポジトリ検査は必須文書欠落とGit追跡されたenvを失敗にする', () => {
  const fixture = mkdtempSync(join(tmpdir(), 'nu-harness-repo-'));
  try {
    execFileSync('git', ['init', '--quiet'], { cwd: fixture });
    writeFileSync(join(fixture, '.env'), 'SAMPLE=fake\n');
    execFileSync('git', ['add', '.env'], { cwd: fixture });
    const errors = repositoryErrors(fixture);
    assert.ok(errors.some(error => error === '必須ファイルなし: docs/REQUIREMENTS.md'));
    assert.ok(errors.some(error => error === 'Gitへ追加しないファイル: .env'));
    assert.ok(errors.every(error => !error.includes('SAMPLE=fake')));
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});

test('CIの実イベントファイルを使い不正PRを非0で終了する（タイトルを実行しない）', () => {
  const fixture = mkdtempSync(join(tmpdir(), 'nu-harness-event-'));
  try {
    const eventPath = join(fixture, 'event.json');
    writeFileSync(eventPath, JSON.stringify({ pull_request: { head: { ref: 'main' }, title: '$(exit 0)' } }));
    const result = spawnSync(process.execPath, ['scripts/check.mjs', '--policy'], {
      cwd: root, encoding: 'utf8', env: { ...process.env, GITHUB_EVENT_PATH: eventPath },
    });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /PRタイトル/);
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});

test('サイト本文の公開URLリンクだけをリンク検査から除外する', () => {
  assert.ok(siteContentPattern.test('apps/web/content/blog/nu.metasou.md'));
  assert.ok(siteContentPattern.test('apps/web/content/project/nu-bar.md'));
  assert.ok(!siteContentPattern.test('apps/web/README.md'));
  assert.ok(!siteContentPattern.test('docs/content/x.md'));
  assert.ok(!siteContentPattern.test('apps/web/docs/x.md'));
});

test('緊急復旧(recover-owner)をworkflowから実行することを禁止する', () => {
  assert.equal(workflowPolicyErrors('.github/workflows/x.yml', 'run: npm test').length, 0);
  assert.equal(workflowPolicyErrors('.github/workflows/x.yml', 'run: docker compose run api /nuctl bootstrap-owner').length, 0);
  assert.equal(workflowPolicyErrors('.github/workflows/deploy.yml', 'run: ssh vps nuctl recover-owner --login-id x').length, 1);
  for (const cmd of ['media-gc --apply', 'media-redact --id x', 'media-verify']) {
    assert.equal(workflowPolicyErrors('.github/workflows/deploy.yml', `run: ssh vps nuctl ${cmd}`).length, 1, cmd);
  }
});
