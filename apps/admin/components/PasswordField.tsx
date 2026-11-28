"use client";

import { useState } from "react";
import IconButton from "@/components/ui/IconButton";
import TextField from "@/components/ui/TextField";

type Props = {
  label: string;
  value: string;
  onChange: (v: string) => void;
  autoComplete: "current-password" | "new-password";
  hint?: string;
  required?: boolean;
};

/**
 * パスワード入力（Charcoalの TextField）。ラベルは常に表示し、枠の中の目のアイコンで表示・非表示を切り替える。
 * 貼り付けは禁止しない。切り替えボタンは読み上げ用の名前と押下状態（aria-pressed）を持つ。
 */
export default function PasswordField({ label, value, onChange, autoComplete, hint, required = true }: Props) {
  const [shown, setShown] = useState(false);
  return (
    <div className="field">
      <TextField
        label={label}
        type={shown ? "text" : "password"}
        value={value}
        onChange={onChange}
        autoComplete={autoComplete}
        required={required}
        assistiveText={hint}
        autoCapitalize="none"
        spellCheck={false}
        suffix={
          <IconButton
            icon={shown ? "24/Hide" : "24/Show"}
            label={shown ? "パスワードを隠す" : "パスワードを表示する"}
            aria-pressed={shown}
            onClick={() => setShown((s) => !s)}
          />
        }
      />
    </div>
  );
}
