"use client";

import { useEffect, useRef } from "react";

import MediaImg from "@/components/MediaImg";
import Button from "@/components/ui/Button";
import { checkImageFile, MAX_ITEM_IMAGES, newKey, type ImageEntry } from "@/lib/item-images";

/**
 * 物品の画像の編集（複数枚・メインの選択・並べ替え・削除・追加前のプレビュー）。
 * 保存は親が lib/item-images の commitImages で行う。ここでは並びを編集するだけ（ファイルはまだ送らない）。
 * 先頭がメイン（一覧のカードに出る画像）。並べ替えはボタン（キーボード・スマホでも操作できる）。
 */
export default function ItemImagesEditor({ entries, onChange, onError, disabled }: { entries: ImageEntry[]; onChange: (e: ImageEntry[]) => void; onError: (text: string) => void; disabled?: boolean }) {
  const input = useRef<HTMLInputElement>(null);
  const urls = useRef<Set<string>>(new Set());
  // 作ったプレビューURLは、画面を離れるときにまとめて解放する
  useEffect(() => { const u = urls.current; return () => u.forEach((x) => URL.revokeObjectURL(x)); }, []);

  function add(files: FileList | null) {
    if (!files || files.length === 0) return;
    const next = [...entries];
    for (const f of Array.from(files)) {
      if (next.length >= MAX_ITEM_IMAGES) { onError(`画像は${MAX_ITEM_IMAGES}枚までです。`); break; }
      const problem = checkImageFile(f);
      if (problem) { onError(problem); continue; }
      const previewUrl = URL.createObjectURL(f);
      urls.current.add(previewUrl);
      next.push({ key: newKey(), kind: "new", file: f, previewUrl });
    }
    onChange(next);
    if (input.current) input.current.value = "";
  }
  const move = (i: number, to: number) => {
    if (to < 0 || to >= entries.length) return;
    const next = [...entries];
    [next[i], next[to]] = [next[to], next[i]];
    onChange(next);
  };
  const makeMain = (i: number) => { const next = [...entries]; const [e] = next.splice(i, 1); onChange([e, ...next]); };
  const remove = (i: number) => onChange(entries.filter((_, j) => j !== i));

  return (
    <div className="images-editor" data-testid="images-editor">
      {entries.length === 0 ? (
        <p className="hint">まだ写真がありません。写真があると、一覧で探しやすくなります。</p>
      ) : (
        <ul className="image-tiles">
          {entries.map((e, i) => (
            <li key={e.key} className="image-tile">
              <div className="tile-img">
                {e.kind === "existing" ? (
                  <MediaImg id={e.mediaId} alt={`写真${i + 1}`} />
                ) : (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={e.previewUrl} alt={`追加する写真${i + 1}`} />
                )}
                {i === 0 && <span className="tile-main">メイン</span>}
                {e.kind === "new" && <span className="tile-new">未保存</span>}
              </div>
              <div className="tile-actions">
                <Button variant="default" disabled={disabled || i === 0} aria-label={`写真${i + 1}を前へ`} onClick={() => move(i, i - 1)}>←</Button>
                <Button variant="default" disabled={disabled || i === entries.length - 1} aria-label={`写真${i + 1}を後ろへ`} onClick={() => move(i, i + 1)}>→</Button>
                {i > 0 && <Button variant="default" disabled={disabled} onClick={() => makeMain(i)}>メインにする</Button>}
                <Button variant="danger" disabled={disabled} aria-label={`写真${i + 1}を削除`} onClick={() => remove(i)}>削除</Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      <div className="actions">
        <label className="visually-hidden" htmlFor="item-images-input">写真を追加する</label>
        <input id="item-images-input" ref={input} type="file" accept="image/jpeg,image/png,image/webp" multiple disabled={disabled || entries.length >= MAX_ITEM_IMAGES} onChange={(ev) => add(ev.target.files)} />
      </div>
      <p className="hint">JPEG・PNG・WebP（1枚10MiBまで・{MAX_ITEM_IMAGES}枚まで）。先頭の写真が一覧に出ます。「保存する」を押すまで反映されません。</p>
    </div>
  );
}
