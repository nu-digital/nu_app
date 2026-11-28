"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import Button from "@/components/ui/Button";
import FeatureIcon from "@/components/ui/FeatureIcon";
import { FEATURES } from "@/lib/features";


/**
 * ポータル共通のヘッダー（Charcoal風。ロゴ・アプリの切り替え・表示名・ログアウト）。
 * アプリの切り替え（nav）はランチャー以外の画面だけに出す。ランチャーではカードが同じ役割を持つため。
 * 全員に同じ項目を出す（権限はAPIが強制する。画面の表示・非表示では制御しない）。
 */
const NAV = FEATURES.map((f) => ({ href: f.href, label: f.key === "profile" ? "プロフィール" : f.title, icon: f.icon }));

type Props = { displayName: string; onLogout: () => void; busy: boolean; showNav?: boolean };

export default function AppHeader({ displayName, onLogout, busy, showNav = true }: Props) {
  const pathname = usePathname() ?? "/";
  return (
    <header className="app-header">
      <div className="app-header-bar">
        <Link className="brand" href="/" aria-label="nu_digital ポータル（ランチャーへ）">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/images/logo.webp" alt="" width={32} height={32} />
          <span className="brand-text">nu_digital ポータル</span>
        </Link>
        <div className="who">
          <span className="name" title={displayName}>
            {displayName}
          </span>
          <Button variant="default" fullWidth={false} onClick={onLogout} disabled={busy}>
            ログアウト
          </Button>
        </div>
      </div>
      {showNav && (
        <nav className="app-nav" aria-label="アプリ">
          <ul>
            {NAV.map((n) => {
              const current = pathname === n.href || pathname.startsWith(n.href + "/") || (n.href === "/admin/users" && pathname.startsWith("/admin/") && !pathname.startsWith("/admin/inventory")) || (n.href === "/inventory" && pathname.startsWith("/admin/inventory"));
              return (
                <li key={n.href}>
                  <Link href={n.href} aria-current={current ? "page" : undefined}>
                    <FeatureIcon name={n.icon} size={16} />{n.label}
                  </Link>
                </li>
              );
            })}
          </ul>
        </nav>
      )}
    </header>
  );
}
