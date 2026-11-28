/**
 * 本文（Markdown）への画像の挿入・差し替え（記事の編集画面。ドラッグ＆ドロップ・貼り付け・ファイル選択で共通）。
 * 画像の記法は独立した段落にする（前後に空行を入れ、すでに空行があれば足さない）。
 */

/** 画像の説明（alt）にするため、ファイル名から拡張子と記号を取り除く。空なら「画像」。 */
export function altFromFileName(name) {
  const base = String(name ?? "").replace(/\.[A-Za-z0-9]{1,5}$/, "").replace(/[\[\]()<>`\\\r\n]/g, " ").replace(/\s+/g, " ").trim();
  return base.slice(0, 60) || "画像";
}

/** 画像の記法。 */
export function imageMarkdown(alt, url) {
  return `![${String(alt).replace(/[\[\]\r\n]/g, " ")}](${url})`;
}

/** アップロード中の目印（プレビューには壊れた画像として出る。保存の前に必ず本物のURLへ差し替わるか、取り除かれる）。 */
export function placeholderUrl(id) {
  return `#uploading-${id}`;
}

/** pos（文字の位置）に text を独立した段落として挿入する。戻り値: 新しい本文と、挿入した直後のカーソル位置。 */
export function insertBlock(markdown, pos, text) {
  const at = Math.max(0, Math.min(pos, markdown.length));
  const before = markdown.slice(0, at);
  const after = markdown.slice(at);
  const sep1 = before === "" ? "" : before.endsWith("\n\n") ? "" : before.endsWith("\n") ? "\n" : "\n\n";
  const sep2 = after === "" ? "\n" : after.startsWith("\n\n") ? "" : after.startsWith("\n") ? "\n" : "\n\n";
  const inserted = sep1 + text + sep2;
  return { markdown: before + inserted + after, caret: before.length + sep1.length + text.length + sep2.length };
}

/** 目印の画像（![…](#uploading-ID)）を、本物の記法に差し替える。見つからなければ変えない。 */
export function replacePlaceholder(markdown, id, replacement) {
  const re = new RegExp(`!\\[[^\\]]*\\]\\(${placeholderUrl(id).replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\)`);
  return re.test(markdown) ? markdown.replace(re, () => replacement) : markdown;
}

/** 目印の画像を取り除く（失敗したとき）。前後の余分な空行も整える。 */
export function removePlaceholder(markdown, id) {
  const re = new RegExp(`\\n*!\\[[^\\]]*\\]\\(${placeholderUrl(id).replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\)\\n*`);
  if (!re.test(markdown)) return markdown;
  return markdown.replace(re, (m, off) => (off === 0 || off + m.length >= markdown.length ? "" : "\n\n"));
}

/** 本文に目印が残っているか（アップロード中のまま保存しないため）。 */
export function hasPlaceholder(markdown) {
  return /\]\(#uploading-[A-Za-z0-9_-]+\)/.test(markdown);
}
