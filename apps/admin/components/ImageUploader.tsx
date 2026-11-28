"use client";

import { useRef, useState } from "react";

import MediaImg from "@/components/MediaImg";
import Button from "@/components/ui/Button";
import { describe, uploadMedia } from "@/lib/api";

/**
 * 画像を選んでアップロードし、所有者（プロフィール・物品）へ設定する小さな部品。
 * 形式・大きさ・寸法の検査と、WebPへの再エンコードはAPIが行う。アップロードできるのはJPEG・PNG・WebP。
 */
export default function ImageUploader({
  label, purpose, currentId, attach, detach, busy, onError, onDone, large,
}: {
  label: string;
  purpose: "avatar" | "inventory";
  currentId: string | null;
  attach: (mediaId: string) => Promise<void>;
  detach: () => Promise<void>;
  busy: boolean;
  onError: (text: string) => void;
  onDone: (text: string) => void;
  large?: boolean;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [working, setWorking] = useState(false);
  const disabled = busy || working;
  async function choose(file: File | undefined) {
    if (!file || disabled) return;
    setWorking(true);
    try {
      const m = await uploadMedia(file, purpose);
      await attach(m.id);
      onDone(`${label}を設定しました。`);
    } catch (e) {
      onError(describe(e));
    } finally {
      setWorking(false);
      if (input.current) input.current.value = "";
    }
  }
  async function remove() {
    if (disabled) return;
    setWorking(true);
    try {
      await detach();
      onDone(`${label}を外しました。`);
    } catch (e) {
      onError(describe(e));
    } finally {
      setWorking(false);
    }
  }
  return (
    <div className="field" data-testid={`uploader-${purpose}`}>
      <span className="label">{label}</span>
      {currentId ? (
        <MediaImg className={large ? "avatar lg" : "photo"} id={currentId} alt={`現在の${label}`} />
      ) : (
        <p className="hint">まだ設定していません。</p>
      )}
      <div className="actions">
        <label className="visually-hidden" htmlFor={`file-${purpose}`}>{currentId ? "画像を変える" : "画像を選ぶ"}</label>
        <input
          id={`file-${purpose}`} ref={input} className="input" type="file" accept="image/jpeg,image/png,image/webp" disabled={disabled}
          onChange={(e) => void choose(e.target.files?.[0])}
        />
        {currentId && <Button variant="default" fullWidth={false} type="button" disabled={disabled} onClick={() => void remove()}>外す</Button>}
      </div>
      {working && <p className="hint" aria-busy="true">処理しています…</p>}
    </div>
  );
}
