import type { Metadata, Viewport } from "next";

// Charcoal（試験導入 #178）: 部品CSS（@layer charcoal）→ トークン（Token v2・ライト）→ 既存の全体CSS → 団体テーマの上書き の順。
import "@charcoal-ui/react/dist/layered.css";
import "@charcoal-ui/theme/css/v2/light.css";
import "./globals.css";
import "./charcoal-theme.css";
import "./ui.css";
import { ToastProvider } from "@/components/ui/Toast";

export const metadata: Metadata = {
  title: "nu_digital ポータル",
  // 内部アプリは検索エンジンに載せない。
  robots: { index: false, follow: false },
};

export const viewport: Viewport = { width: "device-width", initialScale: 1, themeColor: "#004aad" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ja" className="ch-token-v2">
      <body><ToastProvider>{children}</ToastProvider></body>
    </html>
  );
}
