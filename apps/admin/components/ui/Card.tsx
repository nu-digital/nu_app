import type { ReactNode } from "react";

/** 白地の面。ページ内のまとまり（設定の1項目など）に使う。 */
export default function Card({ children, as: Tag = "section", ...rest }: { children: ReactNode; as?: "section" | "div" | "li" } & React.HTMLAttributes<HTMLElement>) {
  return <Tag {...(rest as object)} className={["ui-card", rest.className].filter(Boolean).join(" ")}>{children}</Tag>;
}
