import type { KnownIconType } from "@charcoal-ui/icons";

/**
 * ポータルの機能の一覧（ホームのタイル・ヘッダーのナビで共通）。**同じ機能には、どこでも同じアイコンを使う。**
 * アイコンはCharcoalのもの（24px）。全員に同じ項目を出す（権限はAPIが強制する。画面の表示・非表示では制御しない）。
 */
export type IconName = Extract<keyof KnownIconType, `24/${string}`>;
export type Feature = { key: string; title: string; description: string; href: string; icon: IconName; tone: "blue" | "green" | "orange" | "pink" | "violet" | "gray" };

export const FEATURES: Feature[] = [
  { key: "inventory", title: "物品", description: "機材や本を探して、借りる・返す。", href: "/inventory", icon: "24/Archive", tone: "orange" },
  { key: "contents", title: "記事・作品", description: "活動報告や作品を書いて、公開する。", href: "/contents", icon: "24/Description", tone: "blue" },
  { key: "members", title: "メンバー", description: "メンバーを探して、プロフィールを見る。", href: "/members", icon: "24/Users", tone: "green" },
  { key: "profile", title: "アカウント設定", description: "表示名・アイコン・公開する内容を設定する。", href: "/profile", icon: "24/Person", tone: "violet" },
  { key: "admin", title: "ユーザー管理", description: "ユーザーの追加・停止など（管理者向け）。", href: "/admin/users", icon: "24/Settings", tone: "gray" },
];

/** ショートカット（よく使う操作） */
export const SHORTCUTS: { key: string; label: string; href: string; icon: IconName }[] = [
  { key: "write", label: "新しく書く", href: "/contents/new", icon: "24/Pencil" },
  { key: "borrow", label: "物品を借りる", href: "/inventory", icon: "24/Archive" },
  { key: "mine", label: "借りている物", href: "/inventory?mine=1", icon: "24/Check" },
];
