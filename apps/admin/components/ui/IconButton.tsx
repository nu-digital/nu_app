"use client";

import type { ComponentPropsWithoutRef } from "react";
import "@charcoal-ui/icons"; // <pixiv-icon> の登録。アイコンは同梱の @charcoal-ui/icon-files から読み、外部へは通信しない
import type { KnownIconType } from "@charcoal-ui/icons";
import { Icon } from "@charcoal-ui/react";

/**
 * 団体の共通アイコンボタン（見た目はCharcoalの IconButton と同じクラスを使う）。
 * Charcoalの IconButton は <pixiv-icon> に寸法を付けずに描画するため、要素の登録後に付く style がSSRと食い違い、
 * hydrationの警告が出る。ここでは Icon（fixedSize がSSR時にも寸法を付ける）を使って避ける。
 * 文字のないボタンなので label（読み上げ名）は必須。
 */
type Props = Omit<ComponentPropsWithoutRef<"button">, "children" | "aria-label"> & {
  icon: Extract<keyof KnownIconType, `24/${string}`>; // Charcoalのアイコン名（24px）。一覧は公式Storybook
  label: string;
  size?: "S" | "M";
};

export default function IconButton({ icon, label, size = "M", type = "button", className, ...props }: Props) {
  return (
    <button
      {...props}
      type={type}
      aria-label={label}
      className={["charcoal-icon-button", className].filter(Boolean).join(" ")}
      data-size={size}
      data-variant="Default"
      data-active={false}
    >
      <Icon name={icon} fixedSize={24} aria-hidden="true" />
    </button>
  );
}
