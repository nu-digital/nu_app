import type { ComponentPropsWithoutRef } from "react";

/** 団体の共通テキストエリア。ラベル必須・onChangeは文字列。文字数制限は maxLength を渡すと残り文字数を出す。 */
type Props = Omit<ComponentPropsWithoutRef<"textarea">, "onChange" | "value"> & {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  hint?: string;
};

export default function Textarea({ id, label, value, onChange, hint, maxLength, className, ...rest }: Props) {
  const hintId = hint || maxLength ? `${id}-hint` : undefined;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <textarea {...rest} id={id} className={["input", className].filter(Boolean).join(" ")} value={value} maxLength={maxLength} aria-describedby={hintId} onChange={(e) => onChange(e.target.value)} />
      {hintId && (
        <p className="hint" id={hintId}>
          {hint}{hint && maxLength ? " " : ""}{maxLength ? `（${value.length} / ${maxLength}文字）` : ""}
        </p>
      )}
    </div>
  );
}
