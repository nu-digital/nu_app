"use client";

import { useState } from "react";

import { describe, listMembers, type Member } from "@/lib/api";

/** メンバーを表示名で探して選ぶ（有効なメンバーだけが出る）。共同編集者・制作メンバーの追加に使う。 */
export default function MemberPicker({ label, exclude, onPick, disabled }: { label: string; exclude: string[]; onPick: (m: Member) => void; disabled?: boolean }) {
  const [q, setQ] = useState("");
  const [items, setItems] = useState<Member[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const id = `mp-${label}`;
  async function search() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      setItems((await listMembers(q.trim())).items);
    } catch (e) {
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="picker">
      <div className="field">
        <label htmlFor={id}>{label}</label>
        <div className="actions">
          <input id={id} className="input" type="search" placeholder="表示名で探す" value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void search(); } }} />
          <button type="button" className="btn secondary inline" onClick={search} disabled={busy || disabled}>探す</button>
        </div>
      </div>
      {error && <p className="hint" role="alert">{error}</p>}
      {items && (items.filter((m) => !exclude.includes(m.id)).length === 0 ? <p className="hint">該当するメンバーはいません。</p> : (
        <ul className="items">
          {items.filter((m) => !exclude.includes(m.id)).map((m) => (
            <li key={m.id} className="item" data-member={m.displayName}>
              <strong>{m.displayName}</strong>
              <button type="button" className="btn secondary inline" disabled={disabled} onClick={() => onPick(m)}>{m.displayName}さんを追加</button>
            </li>
          ))}
        </ul>
      ))}
    </div>
  );
}
