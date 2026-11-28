import type { ComponentPropsWithoutRef } from "react";

/**
 * 団体の共通セレクト（ネイティブの <select>。モバイルの標準の選択UIが使えて、キーボード・読み上げに強い）。
 * ラベルは常に表示する。見た目は TextField と同じ（globals.css の .input）。
 */
type Props = Omit<ComponentPropsWithoutRef<"select">, "onChange"> & {
  id: string;
  label: string;
  onChange: (value: string) => void;
  /** ラベルを視覚的に隠す（ツールバーなどで周囲の文脈が分かるとき。読み上げには残る） */
  hideLabel?: boolean;
};

export default function Select({ id, label, onChange, hideLabel, children, className, ...rest }: Props) {
  return (
    <div className="field">
      <label htmlFor={id} className={hideLabel ? "visually-hidden" : undefined}>{label}</label>
      <select {...rest} id={id} className={["input", className].filter(Boolean).join(" ")} onChange={(e) => onChange(e.target.value)}>
        {children}
      </select>
    </div>
  );
}
