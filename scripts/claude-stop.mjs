import { readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { root } from './check.mjs';

export function stopResult(input, verify) {
  if (input.stop_hook_active === true) return null;
  if (verify()) return null;
  return {
    decision: 'block',
    reason: '準備用チェックが失敗しました。node scripts/check.mjs を実行し、修正するか未解決の理由を報告してください。アプリの必要テストは別途実行してください。',
  };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const input = JSON.parse(readFileSync(0, 'utf8'));
    const result = stopResult(input, () => spawnSync(process.execPath, ['scripts/check.mjs'], {
      cwd: root, stdio: 'ignore', timeout: 20000,
    }).status === 0);
    if (result) console.log(JSON.stringify(result));
  } catch {
    console.error('Stop hookを実行できませんでした。node scripts/check.mjs を手動で確認してください。');
    process.exitCode = 1;
  }
}
