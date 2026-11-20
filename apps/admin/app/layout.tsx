import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "nu_app",
  // 内部アプリは検索エンジンに載せない。
  robots: { index: false, follow: false },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ja">
      <body>{children}</body>
    </html>
  );
}
