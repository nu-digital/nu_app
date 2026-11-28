"use client";

import { Button as CharcoalButton, type ButtonProps as CharcoalButtonProps } from "@charcoal-ui/react";

/**
 * 団体の共通ボタン（Charcoalの Button を団体テーマで使う）。試験導入（#178）。
 * 画面からは @charcoal-ui/react を直接importせず、この部品を使う（docs/design/charcoal.md の利用ルール）。
 * 既定は主操作（primary）・横幅いっぱい・type="button"。上流のAPI変更はこのファイルで吸収する。
 */
export type ButtonProps = Omit<CharcoalButtonProps<"button">, "variant" | "size" | "component"> & {
  variant?: "primary" | "default" | "danger";
};

const variants = { primary: "Primary", default: "Default", danger: "Danger" } as const;

export default function Button({ variant = "primary", fullWidth = true, type = "button", ...props }: ButtonProps) {
  return <CharcoalButton {...props} type={type} variant={variants[variant]} fullWidth={fullWidth} />;
}
