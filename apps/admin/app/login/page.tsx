"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";

import Brand from "@/components/Brand";
import PasswordField from "@/components/PasswordField";
import { describe, fetchMe, login } from "@/lib/api";

export default function LoginPage() {
  const router = useRouter();
  const [loginId, setLoginId] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const errorRef = useRef<HTMLDivElement>(null);

  // すでにログイン済みならランチャーへ
  useEffect(() => {
    fetchMe()
      .then((me) => me && router.replace("/"))
      .catch(() => undefined);
  }, [router]);

  useEffect(() => {
    if (error) errorRef.current?.focus(); // エラーを伝えるためフォーカスを移す
  }, [error]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy) return; // 二重送信を防ぐ
    setBusy(true);
    setError("");
    try {
      await login(loginId.trim(), password);
      router.replace("/");
    } catch (err) {
      setError(describe(err)); // 入力は保持する
      setBusy(false);
    }
  }

  return (
    <main className="auth-wrap">
      <div className="auth-card">
        <Brand />
        <h1>ログイン</h1>
        {error && (
          <div className="notice error" role="alert" tabIndex={-1} ref={errorRef}>
            <strong>エラー</strong>
            <span>{error}</span>
          </div>
        )}
        <form onSubmit={onSubmit} noValidate={false}>
          <div className="field">
            <label htmlFor="loginId">ログインID</label>
            <input
              id="loginId"
              className="input"
              type="text"
              value={loginId}
              onChange={(e) => setLoginId(e.target.value)}
              autoComplete="username"
              autoCapitalize="none"
              spellCheck={false}
              required
            />
          </div>
          <PasswordField label="パスワード" value={password} onChange={setPassword} autoComplete="current-password" />
          <button className="btn" type="submit" disabled={busy}>
            {busy ? "ログインしています…" : "ログイン"}
          </button>
        </form>
        <p className="hint" style={{ marginTop: "var(--space-4)", color: "var(--color-muted)" }}>
          ログインIDやパスワードが分からないときは、管理者に連絡してください。
        </p>
      </div>
    </main>
  );
}
