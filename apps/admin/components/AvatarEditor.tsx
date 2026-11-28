"use client";

import { useEffect, useRef, useState } from "react";

import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import Dialog from "@/components/ui/Dialog";
import { clearAvatar, describe, setAvatar, uploadMedia } from "@/lib/api";
import { Avatar } from "@/components/ui/Avatar";

/**
 * プロフィール画像の変更: 画像を選ぶ → 丸いプレビューを見ながら拡大・位置を調整（トリミング）→ 保存。
 * 正方形に切り抜いた画像をアップロードする（形式・大きさの検査と再エンコードはAPI）。画像を外すこともできる。
 */
const OUT = 512;

export default function AvatarEditor({ open, onClose, name, currentId, onDone }: { open: boolean; onClose: () => void; name: string; currentId: string | null; onDone: (text: string) => void }) {
  const [img, setImg] = useState<HTMLImageElement | null>(null);
  const [zoom, setZoom] = useState(1);
  const [dx, setDx] = useState(0);
  const [dy, setDy] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const canvas = useRef<HTMLCanvasElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  // 描画: 画像の短い辺に合わせた正方形を、zoomで拡大した範囲から（dx・dyで動かして）切り抜く
  function draw(target: HTMLCanvasElement, size: number) {
    if (!img) return;
    const ctx = target.getContext("2d");
    if (!ctx) return;
    target.width = size; target.height = size;
    const side = Math.min(img.naturalWidth, img.naturalHeight) / zoom;
    const maxX = (img.naturalWidth - side) / 2, maxY = (img.naturalHeight - side) / 2;
    const sx = img.naturalWidth / 2 - side / 2 + (dx / 100) * maxX;
    const sy = img.naturalHeight / 2 - side / 2 + (dy / 100) * maxY;
    ctx.fillStyle = "#fff"; ctx.fillRect(0, 0, size, size);
    ctx.drawImage(img, sx, sy, side, side, 0, 0, size, size);
  }
  useEffect(() => { if (canvas.current) draw(canvas.current, 240); });

  function pick(f: File | undefined) {
    if (!f) return;
    setError("");
    if (!["image/jpeg", "image/png", "image/webp"].includes(f.type)) { setError("JPEG・PNG・WebPの画像を選んでください。"); return; }
    if (f.size > 10 * 1024 * 1024) { setError("画像が大きすぎます（10MiBまで）。"); return; }
    const url = URL.createObjectURL(f);
    const i = new Image();
    i.onload = () => { setImg(i); setZoom(1); setDx(0); setDy(0); };
    i.onerror = () => setError("画像を読み込めませんでした。");
    i.src = url;
  }

  async function save() {
    if (!img || busy) return;
    setBusy(true);
    setError("");
    try {
      const c = document.createElement("canvas");
      draw(c, OUT);
      const blob: Blob = await new Promise((res, rej) => c.toBlob((b) => (b ? res(b) : rej(new Error("変換に失敗しました"))), "image/png"));
      const m = await uploadMedia(new File([blob], "avatar.png", { type: "image/png" }), "avatar");
      await setAvatar(m.id);
      reset();
      onDone("プロフィール画像を変更しました。");
    } catch (e) {
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (busy) return;
    setBusy(true);
    try {
      await clearAvatar();
      reset();
      onDone("プロフィール画像を外しました。");
    } catch (e) {
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }
  function reset() { setImg(null); setError(""); if (fileRef.current) fileRef.current.value = ""; }

  return (
    <Dialog open={open} title="プロフィール画像" onClose={() => { reset(); onClose(); }}
      actions={<>
        <Button fullWidth={false} disabled={!img || busy} onClick={save}>{busy ? "保存しています…" : "保存する"}</Button>
        {currentId && !img && <Button variant="danger" fullWidth={false} disabled={busy} onClick={remove}>画像を外す</Button>}
      </>}>
      {error && <Notice kind="error">{error}</Notice>}
      <p className="hint">ログイン中のメンバーにだけ表示されます（インターネットには公開されません）。顔が写った写真を使う場合は、公開してよいか確認してください。</p>
      <div className="avatar-edit">
        {img ? <canvas ref={canvas} className="avatar-canvas" aria-label="切り抜きのプレビュー" /> : <Avatar name={name} mediaId={currentId} size={120} />}
      </div>
      <div className="field">
        <label htmlFor="avatar-file">{img ? "別の画像を選ぶ" : "画像を選ぶ（JPEG・PNG・WebP）"}</label>
        <input id="avatar-file" ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" onChange={(e) => pick(e.target.files?.[0])} />
      </div>
      {img && (
        <div className="crop-controls">
          <label htmlFor="crop-zoom">拡大</label><input id="crop-zoom" type="range" min={1} max={3} step={0.05} value={zoom} onChange={(e) => setZoom(Number(e.target.value))} />
          <label htmlFor="crop-x">左右の位置</label><input id="crop-x" type="range" min={-100} max={100} value={dx} onChange={(e) => setDx(Number(e.target.value))} />
          <label htmlFor="crop-y">上下の位置</label><input id="crop-y" type="range" min={-100} max={100} value={dy} onChange={(e) => setDy(Number(e.target.value))} />
        </div>
      )}
    </Dialog>
  );
}
