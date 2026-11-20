import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Dockerイメージを小さくするため、必要なファイルだけを.next/standaloneへ出力する。
  output: "standalone",
  poweredByHeader: false,
};

export default nextConfig;
