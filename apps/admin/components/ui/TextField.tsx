"use client";

import { forwardRef } from "react";
import { TextField as CharcoalTextField, type TextFieldProps as CharcoalTextFieldProps } from "@charcoal-ui/react";

/**
 * 団体の共通テキスト入力（Charcoalの TextField を団体テーマで使う）。試験導入（#178）。
 * ポータルの方針どおりラベルは常に表示する（Charcoalの既定 showLabel=false では視覚的に隠れるため、ここで固定する）。
 * onChange は入力値（string）を受け取る。
 */
export type TextFieldProps = Omit<CharcoalTextFieldProps, "showLabel" | "label"> & { label: string };

const TextField = forwardRef<HTMLInputElement, TextFieldProps>(function TextField(props, ref) {
  return <CharcoalTextField {...props} ref={ref} showLabel />;
});

export default TextField;
