import assert from "node:assert/strict";
import test from "node:test";

import { altFromFileName, hasPlaceholder, imageMarkdown, insertBlock, placeholderUrl, removePlaceholder, replacePlaceholder } from "./markdown-insert.mjs";

test("画像の説明はファイル名から作り、記号を取り除く", () => {
  assert.equal(altFromFileName("IMG_0001.JPG"), "IMG_0001");
  assert.equal(altFromFileName("[x](y).png"), "x y");
  assert.equal(altFromFileName(""), "画像");
  assert.equal(altFromFileName(".png"), "画像");
});

test("挿入は独立した段落にし、空行を重ねない", () => {
  assert.deepEqual(insertBlock("", 0, "![a](u)"), { markdown: "![a](u)\n", caret: 8 });
  assert.equal(insertBlock("本文", 2, "![a](u)").markdown, "本文\n\n![a](u)\n");
  assert.equal(insertBlock("本文\n", 3, "![a](u)").markdown, "本文\n\n![a](u)\n");
  assert.equal(insertBlock("本文\n\n", 4, "![a](u)").markdown, "本文\n\n![a](u)\n");
  assert.equal(insertBlock("前後", 1, "![a](u)").markdown, "前\n\n![a](u)\n\n後");
  assert.equal(insertBlock("前\n\n後", 1, "![a](u)").markdown, "前\n\n![a](u)\n\n後");
  // 範囲外の位置は端に丸める
  assert.equal(insertBlock("abc", 99, "![a](u)").markdown, "abc\n\n![a](u)\n");
  assert.equal(insertBlock("abc", -5, "![a](u)").markdown, "![a](u)\n\nabc");
});

test("目印を本物の記法に差し替え、失敗したら取り除く", () => {
  const id = "k1";
  const ph = imageMarkdown("写真", placeholderUrl(id));
  const md = insertBlock("上\n\n下", 1, ph).markdown;
  assert.ok(hasPlaceholder(md));
  const done = replacePlaceholder(md, id, imageMarkdown("写真", "/api/v1/public/media/abc"));
  assert.ok(!hasPlaceholder(done) && done.includes("![写真](/api/v1/public/media/abc)"));
  assert.equal(replacePlaceholder("目印なし", id, "x"), "目印なし");
  assert.equal(removePlaceholder(md, id), "上\n\n下");
  assert.equal(removePlaceholder(ph, id), "");
  assert.equal(removePlaceholder("目印なし", id), "目印なし");
  // 置換後の文字列に $& などが含まれても、そのまま入る
  assert.ok(replacePlaceholder(md, id, "![a$&b](u)").includes("![a$&b](u)"));
});

test("別のIDの目印は触らない", () => {
  const a = imageMarkdown("a", placeholderUrl("id-a")), b = imageMarkdown("b", placeholderUrl("id-b"));
  const md = `${a}\n\n${b}`;
  assert.equal(removePlaceholder(md, "id-a"), b);
  assert.ok(replacePlaceholder(md, "id-b", "![b](u)").includes(a));
});
