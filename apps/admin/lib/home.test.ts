import assert from "node:assert/strict";
import test from "node:test";

import { heroSlides, imageOf, publicHref, publicSiteBase, recentOf, type PublicItem } from "./home.ts";

const it = (o: Partial<PublicItem> & { slug: string }): PublicItem => ({ type: "article", section: "blog", title: o.slug, summary: "", author: "nu_digital", metadata: {}, date: { label: "2026年1月", kind: "month", sortDate: "2026-01-01" }, ...o });

test("公開サイトのURLは app. を外したホスト。開発中はnull", () => {
  assert.equal(publicSiteBase("app.nu-digital.jp"), "https://nu-digital.jp");
  assert.equal(publicSiteBase("localhost"), null);
  assert.equal(publicSiteBase("127.0.0.1"), null);
  assert.equal(publicHref({ section: "blog", slug: "a" }, "https://nu-digital.jp"), "https://nu-digital.jp/blog/a/");
  assert.equal(publicHref({ section: "blog", slug: "a" }, null), null);
});

test("画像は既存記事の画像（中継）→ カバーの順。無ければnull", () => {
  assert.equal(imageOf({ metadata: { image: "/images/x/y.webp" }, coverUrl: "/api/v1/public/media/abc" }), "/web-images/x/y.webp");
  assert.equal(imageOf({ metadata: {}, coverUrl: "/api/v1/public/media/abc" }), "/api/v1/public/media/abc");
  assert.equal(imageOf({ metadata: { image: "https://evil.example/x.png" } }), null);
  assert.equal(imageOf({ metadata: {} }), null);
});

test("スライドは画像のあるものだけを、新しい順に最大件数まで", () => {
  const items = [it({ slug: "a", coverUrl: "/api/v1/public/media/a" }), it({ slug: "b" }), it({ slug: "c", type: "project", section: "project", metadata: { image: "/images/c.webp" } }), it({ slug: "d", coverUrl: "/api/v1/public/media/d" })];
  const s = heroSlides(items, "https://nu-digital.jp", 2);
  assert.deepEqual(s.map((x) => x.key), ["blog/a", "project/c"]);
  assert.equal(s[1].kind, "作品");
  assert.equal(s[1].href, "https://nu-digital.jp/project/c/");
  assert.equal(heroSlides([], null).length, 0);
});

test("最近の公開は種類ごと", () => {
  const items = [it({ slug: "a" }), it({ slug: "p", type: "project", section: "project" }), it({ slug: "b" })];
  assert.deepEqual(recentOf(items, "article").map((x) => x.slug), ["a", "b"]);
  assert.deepEqual(recentOf(items, "project", 1).map((x) => x.slug), ["p"]);
});
