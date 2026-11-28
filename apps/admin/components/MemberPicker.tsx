"use client";

import { useEffect, useState } from "react";

import Button from "@/components/ui/Button";
import { Avatar } from "@/components/ui/Avatar";
import { describe, listMembers, type Member } from "@/lib/api";

/**
 * メンバーを表示名で探して選ぶ（有効なメンバーだけが出る）。入力が止まると自動で検索する（GitHubのメンバー検索のように）。
 * 候補にはプロフィールアイコンと表示名を出す。共同編集者・制作メンバーの追加に使う。
 */
export default function MemberPicker({ label, exclude, onPick, disabled }: { label: string; exclude: string[]; onPick: (m: Member) => void; disabled?: boolean }) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Member[] | null>(null);
  const [error, setError] = useState("");
  const id = `mp-${label}`;

  useEffect(() => {
    if (!open) return;
    let alive = true;
    const t = setTimeout(() => {
      listMembers(q.trim())
        .then((r) => { if (alive) { setItems(r.items); setError(""); } })
        .catch((e) => { if (alive) setError(describe(e)); });
    }, q ? 300 : 0);
    return () => { alive = false; clearTimeout(t); };
  }, [q, open]);

  const shown = (items ?? []).filter((m) => !exclude.includes(m.id));
  return (
    <div className="picker">
      <div className="field">
        <label htmlFor={id}>{label}</label>
        <input id={id} className="input" type="search" placeholder="表示名で探す" value={q} disabled={disabled} onFocus={() => setOpen(true)} onChange={(e) => { setOpen(true); setQ(e.target.value); }} />
      </div>
      {error && <p className="hint" role="alert">{error}</p>}
      {open && items && (shown.length === 0 ? <p className="hint">該当するメンバーはいません。</p> : (
        <ul className="picker-list" aria-label="候補のメンバー">
          {shown.slice(0, 8).map((m) => (
            <li key={m.id} className="picker-row" data-member={m.displayName}>
              <Avatar name={m.displayName} mediaId={m.avatarMediaId} size={32} />
              <strong className="grow">{m.displayName}</strong>
              <Button variant="default" fullWidth={false} disabled={disabled} aria-label={`${m.displayName}さんを追加`} onClick={() => onPick(m)}>追加</Button>
            </li>
          ))}
        </ul>
      ))}
    </div>
  );
}
