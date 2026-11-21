"use client";

import { useId, useState } from "react";

type Props = {
  label: string;
  value: string;
  onChange: (v: string) => void;
  autoComplete: "current-password" | "new-password";
  hint?: string;
  required?: boolean;
};

/** パスワード入力。ラベルは常に表示し、「表示する」で確認できる（貼り付けは禁止しない）。 */
export default function PasswordField({ label, value, onChange, autoComplete, hint, required = true }: Props) {
  const id = useId();
  const [shown, setShown] = useState(false);
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <div className="input-row">
        <input
          id={id}
          className="input"
          type={shown ? "text" : "password"}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          autoComplete={autoComplete}
          required={required}
          aria-describedby={hint ? `${id}-hint` : undefined}
          autoCapitalize="none"
          spellCheck={false}
        />
        <button type="button" className="toggle" onClick={() => setShown((s) => !s)} aria-pressed={shown}>
          {shown ? "隠す" : "表示する"}
        </button>
      </div>
      {hint && (
        <p className="hint" id={`${id}-hint`}>
          {hint}
        </p>
      )}
    </div>
  );
}
