"use client";

import { useState } from "react";

import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import Dialog from "@/components/ui/Dialog";
import Select from "@/components/ui/Select";
import TextField from "@/components/ui/TextField";
import Textarea from "@/components/ui/Textarea";
import { describe, type Visibility } from "@/lib/api";

/** 設定の1項目を編集するダイアログ（現在の値から始め、保存・キャンセル・入力エラーを表示する）。公開範囲を持つ項目は選択欄が出る。 */
export type FieldSpec = {
  title: string; label: string; value: string; kind?: "text" | "textarea" | "number" | "email" | "url";
  maxLength?: number; hint?: string; required?: boolean; visibility?: Visibility;
  onSave: (value: string, visibility?: Visibility) => Promise<void>;
};

export const VIS_LABEL: Record<Visibility, string> = { private: "非公開（自分と管理者だけ）", members: "メンバーに公開" };

function Body({ spec, onClose }: { spec: FieldSpec; onClose: () => void }) {
  const [value, setValue] = useState(spec.value);
  const [vis, setVis] = useState<Visibility>(spec.visibility ?? "private");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const changed = value !== spec.value || (spec.visibility !== undefined && vis !== spec.visibility);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (busy || !changed) return;
    setBusy(true);
    setError("");
    try {
      await spec.onSave(value.trim(), spec.visibility !== undefined ? vis : undefined);
      onClose();
    } catch (err) {
      setError(describe(err)); // 失敗したら入力を保持して、ダイアログを開いたままにする
    } finally {
      setBusy(false);
    }
  }
  const id = "field-editor";
  return (
    <Dialog
      open title={spec.title} onClose={onClose}
      actions={<Button fullWidth={false} type="submit" form="field-editor-form" disabled={busy || !changed || (spec.required && !value.trim())}>{busy ? "保存しています…" : "保存する"}</Button>}
    >
      <form id="field-editor-form" onSubmit={submit}>
        {error && <Notice kind="error">{error}</Notice>}
        {spec.kind === "textarea" ? (
          <Textarea id={id} label={spec.label} rows={5} maxLength={spec.maxLength} value={value} onChange={setValue} hint={spec.hint} />
        ) : spec.kind === "number" ? (
          <div className="field"><label htmlFor={id}>{spec.label}</label><input id={id} className="input" type="number" min={1} max={10} inputMode="numeric" value={value} onChange={(e) => setValue(e.target.value)} />{spec.hint && <p className="hint">{spec.hint}</p>}</div>
        ) : (
          <div className="field">
            <TextField id={id} label={spec.label} type={spec.kind === "email" ? "email" : "text"} inputMode={spec.kind === "url" ? "url" : undefined} maxLength={spec.maxLength} value={value} onChange={(v: string) => setValue(v)} autoCapitalize={spec.kind === "url" ? "none" : undefined} />
            {spec.hint && <p className="hint">{spec.hint}</p>}
          </div>
        )}
        {spec.visibility !== undefined && (
          <Select id="field-editor-vis" label="公開範囲" value={vis} onChange={(v) => setVis(v as Visibility)}>
            <option value="private">{VIS_LABEL.private}</option>
            <option value="members">{VIS_LABEL.members}</option>
          </Select>
        )}
      </form>
    </Dialog>
  );
}

/** specがnullなら閉じている。開くたびに入力を作り直す。 */
export default function FieldEditor({ spec, onClose }: { spec: FieldSpec | null; onClose: () => void }) {
  if (!spec) return <Dialog open={false} title="" onClose={onClose}>{null}</Dialog>;
  return <Body key={spec.title + spec.value} spec={spec} onClose={onClose} />;
}
