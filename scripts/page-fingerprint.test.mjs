// 指紋の抽出・比較ロジックのテスト（ブラウザー不要）。
import assert from 'node:assert/strict';
import test from 'node:test';
import { compareFingerprints, parseMeta, sha } from './page-fingerprint.mjs';

test('parseMeta: title・description・og・twitter・canonicalを取り出す（属性の順序や自己終了に依存しない）', () => {
  const m = parseMeta('<html><head><title> 記事 | nu </title><meta name="description" content="説明"/><meta content="https://x/y.webp" property="og:image"><meta property="og:image" content="b"><meta name="twitter:card" content="summary_large_image"><meta name="viewport" content="x"><link rel="canonical" href="https://x/blog/a/"></head></html>');
  assert.equal(m.title, '記事 | nu');
  assert.deepEqual(m.description, ['説明']);
  assert.deepEqual(m['og:image'], ['https://x/y.webp', 'b']);
  assert.deepEqual(m['twitter:card'], ['summary_large_image']);
  assert.equal(m.viewport, undefined);
  assert.equal(m.canonical, 'https://x/blog/a/');
});

const page = (url, extra = {}) => ({ url, status: 200, h1: 'x', bodyDomSha: sha('a'), cards: [{ href: '/blog/a/', text: 't', img: '' }], ...extra });

test('compareFingerprints: 同じなら差が無い', () => {
  const b = { pages: [page('/'), page('/blog/')], variants: [{ url: '/blog/a', status: 301, location: '/blog/a/' }] };
  assert.deepEqual(compareFingerprints(b, JSON.parse(JSON.stringify(b))), []);
});

test('compareFingerprints: 項目の差・並びの差・ページの欠落と余分・変種の差を検出する', () => {
  const b = { pages: [page('/'), page('/blog/', { cards: [{ href: '/blog/a/' }, { href: '/blog/b/' }] })], variants: [{ url: '/blog/a', status: 301, location: '/blog/a/' }] };
  const a = { pages: [page('/', { bodyDomSha: sha('b') }), page('/blog/', { cards: [{ href: '/blog/b/' }, { href: '/blog/a/' }] }), page('/extra/')], variants: [{ url: '/blog/a', status: 308, location: '/blog/a/' }] };
  const d = compareFingerprints(b, a).map(x => `${x.url}|${x.field}`).sort();
  assert.deepEqual(d, ['/blog/a|status', '/blog/|cards', '/extra/|基準にないページ', '/|bodyDomSha'].sort());
  const missing = compareFingerprints({ pages: [page('/gone/')], variants: [] }, { pages: [], variants: [] });
  assert.equal(missing[0].field, 'ページが無い');
});

test('compareFingerprints: 意図した差（knownDiffs）だけを許し、それ以外は失敗にする', () => {
  const b = { pages: [page('/404/', { status: 200 })], variants: [{ url: '/blog/x/index.html', status: 200, location: null }] };
  const a = { pages: [page('/404/', { status: 404 })], variants: [{ url: '/blog/x/index.html', status: 404, location: null }] };
  assert.equal(compareFingerprints(b, a).length, 2);
  assert.equal(compareFingerprints(b, a, ['/404/|status', '/blog/x/index.html|status']).length, 0);
  assert.equal(compareFingerprints(b, a, ['/404/|status']).length, 1);
});
