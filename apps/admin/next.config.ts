import type { NextConfig } from "next";

// /api/v1 をAPIへ中継し、ブラウザーから見て同一オリジンにする（本番のNginx構成と同じ）。
// rewritesはビルド時に確定するため、中継先は API_INTERNAL_URL（ビルド引数）で指定する。
const apiInternal = process.env.API_INTERNAL_URL ?? "http://api:8080";

const noStoreNoReferrer = [
  { key: "Referrer-Policy", value: "no-referrer" },
  { key: "Cache-Control", value: "no-store" },
];

const nextConfig: NextConfig = {
  // Dockerイメージを小さくするため、必要なファイルだけを.next/standaloneへ出力する。
  output: "standalone",
  poweredByHeader: false,
  async rewrites() {
    return [{ source: "/api/v1/:path*", destination: `${apiInternal}/api/v1/:path*` }];
  },
  async headers() {
    return [
      // 招待・再設定の画面: トークンを含むURLがRefererや外部に漏れないようにし、キャッシュさせない
      { source: "/invite", headers: noStoreNoReferrer },
      { source: "/reset", headers: noStoreNoReferrer },
      { source: "/login", headers: [{ key: "Cache-Control", value: "no-store" }] },
    ];
  },
};

export default nextConfig;
