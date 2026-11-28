"use client";

import "@charcoal-ui/icons"; // <pixiv-icon> の登録（同梱のアイコンを使い、外部へは通信しない）
import { Icon } from "@charcoal-ui/react";

import type { IconName } from "@/lib/features";

/** 機能のアイコン（飾り。意味は隣の文字が持つので読み上げない）。 */
export default function FeatureIcon({ name, size = 24 }: { name: IconName; size?: 16 | 24 | 32 }) {
  return <Icon name={name} fixedSize={size} aria-hidden="true" />;
}
