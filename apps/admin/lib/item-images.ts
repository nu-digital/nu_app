import { setItemImages, uploadMedia } from "./api";

/** 画像の編集中の1枚。既存（メディアID）か、これからアップロードするファイル（プレビュー用のURLつき）。 */
export type ImageEntry =
  | { key: string; kind: "existing"; mediaId: string }
  | { key: string; kind: "new"; file: File; previewUrl: string };

export const MAX_ITEM_IMAGES = 8;
export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;

let seq = 0;
export const newKey = () => `img-${++seq}`;

export const fromExisting = (ids: string[]): ImageEntry[] => ids.map((mediaId) => ({ key: newKey(), kind: "existing" as const, mediaId }));

/** 画像ファイルの簡易検査（本当の検査・再エンコードはAPI）。問題があれば利用者向けの文言を返す。 */
export function checkImageFile(f: File): string | null {
  if (!["image/jpeg", "image/png", "image/webp"].includes(f.type)) return `「${f.name}」は使えない形式です（JPEG・PNG・WebPのみ）。`;
  if (f.size > MAX_IMAGE_BYTES) return `「${f.name}」が大きすぎます（10MiBまで）。`;
  return null;
}

/**
 * 編集した画像の並びを保存する: 新しいファイルを順にアップロードし、メディアIDの並びで置き換える。
 * アップロード後に置き換えが失敗しても、アップロード済みの画像は参照されないので24時間後に自動で消える。
 */
export async function commitImages(itemId: string, entries: ImageEntry[], onProgress?: (done: number, total: number) => void): Promise<string[]> {
  const total = entries.filter((e) => e.kind === "new").length;
  let done = 0;
  const ids: string[] = [];
  for (const e of entries) {
    if (e.kind === "existing") {
      ids.push(e.mediaId);
    } else {
      const m = await uploadMedia(e.file, "inventory");
      ids.push(m.id);
      onProgress?.(++done, total);
    }
  }
  return (await setItemImages(itemId, ids)).images;
}
