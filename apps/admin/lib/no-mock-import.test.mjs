// 開発用モック（apps/admin/mock）がアプリ本体に混入しないことの検査。
// アプリのコード（app・components・lib）からmockをimportしていないこと、Dockerイメージから除外していることを確かめる。
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = fileURLToPath(new URL('..', import.meta.url));
const walk = (dir) => readdirSync(dir).flatMap((n) => {
  const p = join(dir, n);
  return statSync(p).isDirectory() ? walk(p) : /\.(ts|tsx|mjs|js)$/.test(n) ? [p] : [];
});

test('アプリのコードは開発用モックをimportしない', () => {
  const bad = ['app', 'components', 'lib'].flatMap((d) => walk(join(root, d)))
    .filter((f) => !f.endsWith('no-mock-import.test.mjs'))
    .filter((f) => /from\s+["'][^"']*\bmock\b[^"']*["']|import\(\s*["'][^"']*\bmock\b/.test(readFileSync(f, 'utf8')));
  assert.deepEqual(bad, []);
});

test('Dockerイメージから開発用モックを除外している', () => {
  const lines = readFileSync(join(root, '.dockerignore'), 'utf8').split(/\r?\n/).map((l) => l.trim());
  assert.ok(lines.includes('mock'));
});
