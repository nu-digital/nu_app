import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { dirname, resolve, relative, isAbsolute } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';

export const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const types = '(?:feat|fix|docs|refactor|test|chore|ci)';
const branchPattern = new RegExp(`^${types}/[1-9][0-9]*-[a-z0-9]+(?:-[a-z0-9]+)*$`);
const titlePattern = new RegExp(`^${types}(?:\\([a-z0-9-]+\\))?: .+`);

export function policyErrors(pr) {
  if (!pr) return [];
  // Dependabot generates its own branch/title. A person still reviews and uses a Japanese squash title.
  if (pr.user?.login === 'dependabot[bot]' && pr.head?.ref?.startsWith('dependabot/')) return [];
  const errors = [];
  if (pr.head?.ref !== 'chore/bootstrap-agent-harness' && !branchPattern.test(pr.head?.ref ?? '')) {
    errors.push('ブランチ名は <type>/<Issue番号>-<英小文字の要約> にしてください。');
  }
  if (!titlePattern.test(pr.title ?? '') || !/[\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Han}]/u.test(pr.title ?? '')) {
    errors.push('PRタイトルは type(scope任意): 日本語の変更内容 にしてください。');
  }
  return errors;
}

// Ownerの緊急復旧(recover-owner)は、運用責任者がサーバー上で手動で実行する手順であり、
// GitHub Actionsのworkflow（デプロイ等）から呼んではならない（D05。docs/OPERATIONS.md）。
// 画像の運用コマンド(media-gc・media-redact・media-verify)も、運用者が手動で実行する（削除・無効化を自動化しない）。
export function workflowPolicyErrors(file, content) {
  const errors = [];
  if (/recover-owner/.test(content)) errors.push(`${file}: 緊急復旧(recover-owner)をworkflowから実行してはいけません`);
  if (/import-legacy/.test(content)) errors.push(`${file}: 既存記事の移行(import-legacy)をworkflowから実行してはいけません（運用者が手動で、本番では確認のうえ実行する）`);
  if (/media-(gc|redact|verify)/.test(content)) errors.push(`${file}: 画像の運用コマンド(media-gc・media-redact・media-verify)をworkflowから実行してはいけません`);
  return errors;
}

export function forbiddenPath(file) {
  const normalized = file.replaceAll('\\', '/');
  const name = normalized.split('/').at(-1);
  const exampleEnv = name === '.env.example' || /^\.env\..+\.example$/.test(name);
  return (name.startsWith('.env') && !exampleEnv)
    || /\.(pem|key|p12|dump|sql\.gz)$/i.test(name)
    || /(^|\/)(node_modules|\.next|\.local)(\/|$)/.test(normalized)
    || normalized === '.claude/settings.local.json' || name === 'CLAUDE.local.md';
}

// サイト本文(Markdown)の「/blog/..」「/images/..」はリポジトリ内ファイルへのリンクではなく、公開URLを指す。
// 検証は scripts/web-verify.mjs（記事・画像・ルートの照合）が担当する。
export const siteContentPattern = /^apps\/web\/content\//;

export function linkErrors(file, content, repositoryRoot = root) {
  const errors = [];
  // Check local inline Markdown links, including images. External URLs/anchors and fenced samples are excluded.
  const prose = content.replace(/^```[^\n]*\n[\s\S]*?^```\s*$/gm, '');
  for (const match of prose.matchAll(/\[[^\]\n]*\]\(([^)\n]+)\)/g)) {
    const target = match[1].trim().replace(/^<|>$/g, '');
    if (/^[a-z][a-z0-9+.-]*:/i.test(target) || target.startsWith('#')) continue;
    let path;
    try { path = decodeURIComponent(target.split(/[?#]/)[0]); }
    catch { errors.push(`${file}: 不正なリンクエンコード`); continue; }
    const full = resolve(repositoryRoot, dirname(file), path);
    const rel = relative(repositoryRoot, full);
    if (rel === '..' || rel.startsWith('../') || rel.startsWith('..\\') || isAbsolute(rel) || !existsSync(full)) {
      errors.push(`${file}: ローカルリンク先が存在しないかリポジトリ外です: ${path}`);
    }
  }
  return errors;
}

export function repositoryErrors(repositoryRoot = root) {
  const errors = [];
  const required = [
    'README.md', 'AGENTS.md', 'CLAUDE.md', '.node-version', '.gitignore', '.gitattributes',
    ...['REQUIREMENTS', 'ARCHITECTURE', 'DATABASE', 'API', 'TASKS', 'DEVELOPMENT', 'TESTING',
      'DESIGN', 'OPERATIONS', 'DECISIONS', 'LEGACY_SITE', 'BACKLOG', 'CLAUDE_HANDOFF'].map(name => `docs/${name}.md`),
    '.claude/settings.json', '.github/workflows/harness.yml', '.github/pull_request_template.md',
    '.github/ISSUE_TEMPLATE/task.yml', '.github/ISSUE_TEMPLATE/bug.yml', 'docs/handoffs/TEMPLATE.md',
  ];
  for (const file of required) {
    if (!existsSync(resolve(repositoryRoot, file))) errors.push(`必須ファイルなし: ${file}`);
  }
  const files = execFileSync('git', ['ls-files', '-z', '--cached', '--others', '--exclude-standard'], {
    cwd: repositoryRoot, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024,
  }).split('\0').filter(Boolean);
  for (const file of new Set(files)) {
    if (forbiddenPath(file)) errors.push(`Gitへ追加しないファイル: ${file}`);
    if (!existsSync(resolve(repositoryRoot, file))) {
      errors.push(`削除をGitへ反映してください: ${file}`);
      continue;
    }
    if (file.endsWith('.md') && !siteContentPattern.test(file.replaceAll('\\', '/'))) errors.push(...linkErrors(file, readFileSync(resolve(repositoryRoot, file), 'utf8'), repositoryRoot));
  }
  if (existsSync(resolve(repositoryRoot, 'CLAUDE.md')) && !readFileSync(resolve(repositoryRoot, 'CLAUDE.md'), 'utf8').includes('@AGENTS.md')) {
    errors.push('CLAUDE.mdからAGENTS.mdをimportしてください。');
  }
  const settingsPath = resolve(repositoryRoot, '.claude/settings.json');
  if (existsSync(settingsPath)) {
    const settings = JSON.parse(readFileSync(settingsPath, 'utf8'));
    if (!settings.hooks?.Stop?.some(group => group.hooks?.some(hook => hook.type === 'command'
      && hook.command === 'node "$CLAUDE_PROJECT_DIR/scripts/claude-stop.mjs"' && hook.timeout === 30))) {
      errors.push('Stop hookの設定を確認してください。');
    }
  }
  const workflowDir = resolve(repositoryRoot, '.github/workflows');
  if (existsSync(workflowDir)) {
    for (const name of readdirSync(workflowDir).filter(n => n.endsWith('.yml'))) {
      errors.push(...workflowPolicyErrors(`.github/workflows/${name}`, readFileSync(resolve(workflowDir, name), 'utf8')));
    }
  }
  const workflow = resolve(repositoryRoot, '.github/workflows/harness.yml');
  if (existsSync(workflow)) {
    const yaml = readFileSync(workflow, 'utf8');
    for (const match of yaml.matchAll(/uses:\s*([^\s#]+)/g)) {
      if (!/^[\w-]+\/[\w-]+@[a-f0-9]{40}$/.test(match[1])) errors.push('外部Actionは完全なSHAで固定してください。');
    }
  }
  return errors;
}

function main() {
  const policy = process.argv.includes('--policy');
  const event = process.env.GITHUB_EVENT_PATH ? JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8')) : {};
  const errors = policy ? policyErrors(event.pull_request) : repositoryErrors();
  if (errors.length) {
    for (const error of errors) console.error(`FAIL: ${error}`);
    process.exitCode = 1;
  } else {
    console.log(policy ? 'PASS: PR命名チェック（PR以外のイベントは対象外）' : 'PASS: 準備用リポジトリチェック（アプリテストは含まない）');
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(); }
  catch (error) { console.error(`FAIL: チェック実行エラー (${error.code ?? error.name})`); process.exitCode = 1; }
}
