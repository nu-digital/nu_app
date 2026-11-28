"use client";

import { forwardRef, useCallback, useEffect, useRef } from "react";
import { TextField as CharcoalTextField, type TextFieldProps as CharcoalTextFieldProps } from "@charcoal-ui/react";

/**
 * 団体の共通テキスト入力（Charcoalの TextField を団体テーマで使う）。
 * - ラベルは常に表示する（Charcoalの既定 showLabel=false では視覚的に隠れるため、ここで固定する）
 * - onChange は入力値（string）を受け取る
 * - required: Charcoalは required をラベルの表示にだけ使い、<input> には付けない。
 *   ブラウザーの必須入力の検査を保つため、ここで <input required> を付け直す
 * 数値・日付・ファイル・読み取り専用の入力は、ネイティブの <input className="input"> を使う（見た目はCSSでそろえてある）。
 */
export type TextFieldProps = Omit<CharcoalTextFieldProps, "showLabel" | "label"> & { label: string };

const TextField = forwardRef<HTMLInputElement, TextFieldProps>(function TextField({ required, ...props }, ref) {
  const inner = useRef<HTMLInputElement | null>(null);
  const setRef = useCallback(
    (el: HTMLInputElement | null) => {
      inner.current = el;
      if (typeof ref === "function") ref(el);
      else if (ref) ref.current = el;
    },
    [ref],
  );
  useEffect(() => {
    if (inner.current) inner.current.required = Boolean(required);
  }, [required]);
  return <CharcoalTextField {...props} ref={setRef} showLabel />;
});

export default TextField;
