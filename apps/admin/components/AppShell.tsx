"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";

import { fetchMe, logout, type Me } from "@/lib/api";

/** ログイン必須の画面の共通枠（ヘッダー・ログアウト）。未ログインならログイン画面へ送る。 */
export default function AppShell({ children }: { children: (me: Me) => ReactNode }) {
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    fetchMe()
      .then((m) => {
        if (!alive) return;
        if (!m) router.replace("/login");
        else setMe(m);
      })
      .catch(() => alive && router.replace("/login"));
    return () => {
      alive = false;
    };
  }, [router]);

  if (!me) return <main aria-busy="true" />;
  return (
    <>
      <header className="app-header">
        <Link className="brand" href="/">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/images/logo.webp" alt="" width={36} height={36} />
          <span className="brand-text">nu_digital ポータル</span>
        </Link>
        <div className="who">
          <Link className="btn secondary inline" href="/">
            ランチャーへ
          </Link>
          <button
            className="btn secondary inline"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await logout();
              } finally {
                router.replace("/login");
              }
            }}
          >
            ログアウト
          </button>
        </div>
      </header>
      {children(me)}
    </>
  );
}
