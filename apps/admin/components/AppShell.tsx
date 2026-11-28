"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";

import AppHeader from "@/components/AppHeader";
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
      <AppHeader
        displayName={me.user.displayName}
        busy={busy}
        onLogout={async () => {
          setBusy(true);
          try {
            await logout();
          } finally {
            router.replace("/login");
          }
        }}
      />
      {children(me)}
    </>
  );
}
