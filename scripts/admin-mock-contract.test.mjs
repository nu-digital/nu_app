// 契約比較（scripts/admin-mock-contract.mjs）の検査と、比較対象のAPIがモックですべて応答することの確認。
import assert from 'node:assert/strict';
import test from 'node:test';

import { CONTRACT_PATHS, compareShapes } from './admin-mock-contract.mjs';

test('形の比較: キーの過不足と型の違いを検出し、値の違い・nullは無視する', () => {
  assert.deepEqual(compareShapes({ a: 1, b: { c: 'x' } }, { a: 2, b: { c: 'y' } }), []);
  assert.deepEqual(compareShapes({ a: 1 }, { a: 1, z: 2 }), ['$.z: 実APIにあり、モックに無い']);
  assert.deepEqual(compareShapes({ a: 1, y: 2 }, { a: 1 }), ['$.y: モックにあり、実APIに無い']);
  assert.deepEqual(compareShapes({ a: 1, y: 2 }, { a: 1 }, ['y']), []);
  assert.deepEqual(compareShapes({ a: [] }, { a: {} }), ['$.a: モック=array 実API=object']);
  assert.deepEqual(compareShapes({ a: null }, { a: { b: 1 } }), []);
  assert.deepEqual(compareShapes({ items: [{ id: 1 }] }, { items: [{ id: 2, extra: 1 }] }), ['$.items[0].extra: 実APIにあり、モックに無い']);
  // metadata は記事ごとに自由なキーを持つため、中のキーは比べない（オブジェクトであることだけ）
  assert.deepEqual(compareShapes({ metadata: { category: 'x' } }, { metadata: { image: 'y' } }), []);
  assert.deepEqual(compareShapes({ metadata: {} }, { metadata: [] }), ['$.metadata: モック=object 実API=array']);
});

test('比較対象のAPIは、モックがすべて200で応答する', async () => {
  const { createMock } = await import('../apps/admin/mock/handler.ts');
  const mock = createMock();
  const first = (p) => mock('GET', '/api/v1' + p, 'owner').json?.items?.[0]?.id;
  const ids = { member: first('/members'), item: first('/inventory/items'), content: first('/contents') };
  const bad = CONTRACT_PATHS.map((p) => [p, mock('GET', '/api/v1' + p.replace(/:(\w+)/, (_, k) => ids[k]), 'owner').status]).filter(([, s]) => s !== 200);
  assert.deepEqual(bad, []);
});
