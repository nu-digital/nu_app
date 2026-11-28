import type { ComponentPropsWithoutRef } from "react";

/** 団体の共通チェックボックス（ネイティブ。タップ対象44px）。 */
type Props = Omit<ComponentPropsWithoutRef<"input">, "type" | "onChange" | "checked"> & {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
};

export default function Checkbox({ label, checked, onChange, ...rest }: Props) {
  return (
    <div className="field">
      <label>
        <input {...rest} type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} /> {label}
      </label>
    </div>
  );
}
