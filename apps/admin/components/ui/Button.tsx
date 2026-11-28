"use client";

import Link from "next/link";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import { Button as CharcoalButton } from "@charcoal-ui/react";

/**
 * 団体の共通ボタン（Charcoalの Button を団体テーマで使う）。
 * 画面からは @charcoal-ui/react を直接importせず、この部品を使う（docs/design/charcoal.md の利用ルール）。
 * - variant: primary（主操作。既定）／default（副操作）／danger（取り消せない操作）
 * - fullWidth: 既定は横幅いっぱい。並べるボタンは fullWidth={false}
 * - href を渡すとページへのリンク（Next.jsのLink）になる。見た目は同じ
 * - type の既定は "button"。フォームの送信は type="submit" を明示する
 * 上流のAPI変更はこのファイルで吸収する。
 */
type Variant = "primary" | "default" | "danger";
type Common = { variant?: Variant; fullWidth?: boolean; children?: ReactNode };
type AsButton = Common & Omit<ComponentPropsWithoutRef<"button">, "children"> & { href?: undefined };
type AsLink = Common & Omit<ComponentPropsWithoutRef<typeof Link>, "children" | "href"> & { href: string };
export type ButtonProps = AsButton | AsLink;

const variants = { primary: "Primary", default: "Default", danger: "Danger" } as const;

export default function Button(props: ButtonProps) {
  if (props.href !== undefined) {
    const { variant = "primary", fullWidth = true, href, ...rest } = props;
    return <CharcoalButton {...rest} component={Link} href={href} variant={variants[variant]} fullWidth={fullWidth} />;
  }
  const { variant = "primary", fullWidth = true, type = "button", ...rest } = props;
  return <CharcoalButton {...rest} type={type} variant={variants[variant]} fullWidth={fullWidth} />;
}
