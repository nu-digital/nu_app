"use client";

import { useCallback, useRef, useState, type RefObject } from "react";

import { describe, uploadMedia } from "@/lib/api";
import { altFromFileName, imageMarkdown, insertBlock, placeholderUrl, removePlaceholder, replacePlaceholder } from "@/lib/markdown-insert.mjs";

export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
const TYPES = ["image/jpeg", "image/png", "image/webp"];
const CONCURRENCY = 3;

export type UploadItem = {
  id: string; name: string; size: number; file: File; status: "uploading" | "done" | "error"; progress: number;
  error?: string; /** 最適化後のサイズ（バイト）。完了したら入る */ optimized?: number;
};

/** アップロード前の検査（サーバーも同じ検査をする。ここは待たせずに分かりやすく断るため）。 */
export function checkImageFile(f: File): string | null {
  if (!TYPES.includes(f.type)) {
    if (f.type === "image/gif") return "GIFなどのアニメーション画像には対応していません（JPEG・PNG・WebPのみ）。";
    return "JPEG・PNG・WebPの画像だけアップロードできます。";
  }
  if (f.size > MAX_IMAGE_BYTES) return "10MiBを超える画像はアップロードできません。";
  return null;
}

/** 画像ファイルだけを取り出す（ドロップ・貼り付けのファイル一覧から。画像以外が混ざっていても無視する）。 */
export function imageFiles(list: FileList | File[] | null | undefined): File[] {
  return Array.from(list ?? []).filter((f) => f.type.startsWith("image/"));
}

/**
 * 記事の本文への画像の追加（ドラッグ＆ドロップ・貼り付け・ファイル選択。複数枚）。
 * 追加するとカーソル位置にアップロード中の目印を入れ、アップロードが終わったら本物の画像の記法に差し替える。
 * 失敗した画像は目印を取り除き、一覧に「再試行」を出す。アップロード中かどうか（busy）で、保存を止められる。
 */
export function useBodyImages(opts: { textarea: RefObject<HTMLTextAreaElement | null>; getText: () => string; update: (fn: (md: string) => string) => void }) {
  const { textarea, getText, update } = opts;
  const [items, setItems] = useState<UploadItem[]>([]);
  const seq = useRef(0);
  const running = useRef(0);
  const queue = useRef<string[]>([]);
  const files = useRef(new Map<string, File>());

  const patch = useCallback((id: string, p: Partial<UploadItem>) => setItems((cur) => cur.map((x) => (x.id === id ? { ...x, ...p } : x))), []);

  // 同時に CONCURRENCY 枚まで。空いた担当が次の画像を取りに行く
  const pump = useCallback(() => {
    const worker = async () => {
      while (queue.current.length > 0) {
        const id = queue.current.shift()!;
        const file = files.current.get(id);
        if (!file) continue;
        try {
          const m = await uploadMedia(file, "cms", (r) => patch(id, { progress: r }));
          update((md) => replacePlaceholder(md, id, imageMarkdown(altFromFileName(file.name), m.publicUrl)));
          patch(id, { status: "done", progress: 1, optimized: m.byteSize });
          setTimeout(() => setItems((cur) => cur.filter((x) => !(x.id === id && x.status === "done"))), 8000);
        } catch (e) {
          update((md) => removePlaceholder(md, id));
          patch(id, { status: "error", error: describe(e) });
        }
      }
      running.current--;
    };
    while (running.current < CONCURRENCY && queue.current.length > 0) {
      running.current++;
      void worker();
    }
  }, [patch, update]);

  /** 本文のカーソル位置から順に、画像を追加する。 */
  const addFiles = useCallback((list: File[]) => {
    const el = textarea.current;
    let caret = el ? el.selectionStart : Number.MAX_SAFE_INTEGER;
    let md = getText();
    for (const file of list) {
      const id = `u${Date.now().toString(36)}${(seq.current++).toString(36)}`;
      const err = checkImageFile(file);
      const base: UploadItem = { id, name: file.name || "貼り付けた画像", size: file.size, file, status: err ? "error" : "uploading", progress: 0, error: err ?? undefined };
      setItems((cur) => [...cur, base]);
      if (err) continue;
      files.current.set(id, file);
      const r = insertBlock(md, caret, imageMarkdown("アップロード中…", placeholderUrl(id)));
      md = r.markdown;
      caret = r.caret;
      queue.current.push(id);
    }
    if (md !== getText()) update(() => md);
    pump();
  }, [pump, textarea, getText, update]);

  const retry = useCallback((id: string) => {
    const it = items.find((x) => x.id === id);
    if (!it) return;
    setItems((cur) => cur.filter((x) => x.id !== id));
    addFiles([it.file]);
  }, [items, addFiles]);

  const dismiss = useCallback((id: string) => { files.current.delete(id); setItems((cur) => cur.filter((x) => x.id !== id)); }, []);

  return { items, addFiles, retry, dismiss, busy: items.some((x) => x.status === "uploading") };
}
