"use client";

import { useEffect, useRef, useState, useSyncExternalStore, type FormEvent } from "react";
import Link from "next/link";

import Brand from "@/components/Brand";
import PasswordField from "@/components/PasswordField";
import { describe, submitToken } from "@/lib/api";

type Props = { kind: "accept-invite" | "reset-password"; title: string; lead: string; done: string };

// URLのフラグメント(#token=)から読んだトークン。画面を開いた最初の1回だけ読み、アドレスバー・履歴から消す。
// サーバー描画中は null（何も表示しない）、ブラウザーでは文字列（無ければ空文字）。
let cachedToken: string | null = null;
function readFragmentToken(): string {
  if (cachedToken === null) {
    const m = /(?:^#|&)token=([A-Za-z0-9_-]+)/.exec(window.location.hash);
    cachedToken = m ? m[1] : "";
    if (m) window.history.replaceState(null, "", window.location.pathname);
  }
  return cachedToken;
}
function useFragmentToken(): string | null {
  return useSyncExternalStore(
    () => () => undefined,
    readFragmentToken,
    () => null,
  );
}

/** 招待受諾・再設定の共通フォーム。トークンはURLのフラグメント(#token=)から読み、画面に表示しない。 */
export default function TokenPasswordForm({ kind, title, lead, done }: Props) {
  const token = useFragmentToken();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [finished, setFinished] = useState(false);
  const errorRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy || !token) return;
    if (password !== confirm) {
      setError("確認用のパスワードが一致しません。");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await submitToken(kind, token, password);
      setFinished(true);
    } catch (err) {
      setError(describe(err));
      setBusy(false);
    }
  }

  return (
    <main className="auth-wrap">
      <div className="auth-card">
        <Brand />
        <h1>{title}</h1>
        {token === null ? null : finished ? (
          <>
            <div className="notice success" role="status">
              <strong>完了</strong>
              <span>{done}</span>
            </div>
            <Link className="btn" href="/login">
              ログイン画面へ
            </Link>
          </>
        ) : !token ? (
          <div className="notice error" role="alert">
            <strong>エラー</strong>
            <span>リンクが正しくありません。管理者から届いたURLをそのまま開いてください。</span>
          </div>
        ) : (
          <>
            <p>{lead}</p>
            {error && (
              <div className="notice error" role="alert" tabIndex={-1} ref={errorRef}>
                <strong>エラー</strong>
                <span>{error}</span>
              </div>
            )}
            <form onSubmit={onSubmit}>
              <PasswordField label="新しいパスワード" value={password} onChange={setPassword} autoComplete="new-password" hint="10文字以上128文字以内。ログインIDと同じものや、よく使われるものは使えません。" />
              <PasswordField label="新しいパスワード（確認）" value={confirm} onChange={setConfirm} autoComplete="new-password" />
              <button className="btn" type="submit" disabled={busy}>
                {busy ? "送信しています…" : "パスワードを設定する"}
              </button>
            </form>
          </>
        )}
      </div>
    </main>
  );
}
