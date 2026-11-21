import type { NextConfig } from "next";

// 公開コンテンツは公開API（Go）から描画時に取得する（動的配信。docs/design/p3-web.md §3）。
// キャッシュ設定（revalidate・"use cache"・unstable_cache・cacheComponents・staleTimes・force-static）は使わない
// （撤回・個人情報削除を次のリクエストから反映するため。scripts/check.mjs が禁止する）。
const nextConfig: NextConfig = {
  output: "standalone",
  trailingSlash: true,
  basePath: process.env.NEXT_PUBLIC_BASE_PATH || "",
  poweredByHeader: false,
  images: { unoptimized: true },
  // アップロード画像の公開配信だけを、APIへ中継する（ローカル・smoke用。本番はNginxで同じ経路だけを中継する。P4-01）
  async rewrites() {
    const api = (process.env.API_INTERNAL_URL ?? "http://127.0.0.1:8080").replace(/\/$/, "");
    return [{ source: "/api/v1/public/media/:id([A-Za-z0-9_-]{22})", destination: `${api}/api/v1/public/media/:id` }];
  },
};

export default nextConfig;
