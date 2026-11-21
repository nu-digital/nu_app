"use client";

// 公開APIが使えないとき等の表示。サーバー側ではNextが500を返し、`no-store`にする（エラーページをキャッシュさせない）。
export default function ErrorPage({ reset }: { error: Error; reset: () => void }) {
  return (
    <main className="mx-auto max-w-2xl px-5 py-24 text-center">
      <h1 className="text-3xl font-black">ただいま表示できません</h1>
      <p className="mt-4 text-slate-600">しばらくしてからもう一度お試しください。</p>
      <button type="button" onClick={reset} className="mt-8 border border-slate-300 px-5 py-2 text-sm font-bold">
        再読み込み
      </button>
    </main>
  );
}
