"use client";

/**
 * 表示の切り替え（PC幅/スマホ幅、編集/プレビューなど）。ラジオボタンの意味（どれか1つ）で、矢印キーで移動できる。
 */
export default function SegmentedControl<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: { value: T; label: string }[]; onChange: (v: T) => void }) {
  return (
    <div className="ui-segmented" role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <label key={o.value} className={o.value === value ? "on" : undefined}>
          <input type="radio" name={label} value={o.value} checked={o.value === value} onChange={() => onChange(o.value)} />
          {o.label}
        </label>
      ))}
    </div>
  );
}
