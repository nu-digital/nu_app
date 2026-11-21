import { NextResponse, type NextRequest } from "next/server";

// trailingSlash: true の下では、ドットを含むslug（例: /blog/nu.metasou/）がファイル扱いになり、末尾スラッシュを外す308になる。
// 既存のURL（末尾スラッシュ付き）をそのまま200で配信するため、内部ではスラッシュ無しのパスへrewriteする。
export function proxy(request: NextRequest) {
  const url = new URL(request.url); // nextUrl は末尾のスラッシュを落とすため、生のURLから判定する
  const { pathname } = url;
  const last = pathname.split("/").filter(Boolean).pop() ?? "";
  if (pathname.length > 1 && pathname.endsWith("/") && last.includes(".") && /^\/(blog|news|project)\//.test(pathname)) {
    url.pathname = pathname.slice(0, -1);
    return NextResponse.rewrite(url);
  }
  return NextResponse.next();
}

export const config = { matcher: ["/(blog|news|project)/:path*"] };
