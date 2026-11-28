"use client";

import { useState } from "react";

import MemberPicker from "@/components/MemberPicker";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import { describe, putCollaborators } from "@/lib/api";

type Person = { userId: string; displayName: string };

/** 共同編集者。作者とEditor以上だけが変更できる（共同編集者は閲覧のみ。権限はAPIが強制）。変更はすぐに反映される。 */
export default function CollaboratorsPanel({ contentId, creator, initial, canManage }: { contentId: string; creator: Person; initial: Person[]; canManage: boolean }) {
  const [list, setList] = useState<Person[]>(initial);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);

  async function apply(next: Person[], done: string) {
    if (busy) return;
    setBusy(true);
    setMsg(null);
    try {
      const r = await putCollaborators(contentId, next.map((p) => p.userId));
      setList(r.collaborators);
      setMsg({ kind: "success", text: done });
    } catch (e) {
      setMsg({ kind: "error", text: describe(e) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel" aria-labelledby="collab-title">
      <h2 id="collab-title">共同編集者</h2>
      <p className="hint">作者（{creator.displayName}さん）のほかに、この記事・作品を編集できる人です。公開はできません（公開できるのは編集者以上）。{canManage ? "" : "変更できるのは作者と編集者です。"}</p>
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}
      {list.length === 0 ? <p>共同編集者はいません。</p> : (
        <ul className="items" data-testid="collaborators">
          {list.map((p) => (
            <li key={p.userId} className="item" data-person={p.displayName}>
              <strong>{p.displayName}</strong>
              {canManage && <Button variant="default" fullWidth={false} type="submit" disabled={busy} onClick={() => apply(list.filter((x) => x.userId !== p.userId), `${p.displayName}さんを共同編集者から外しました。`)}>{p.displayName}さんを外す</Button>}
            </li>
          ))}
        </ul>
      )}
      {canManage && (
        <MemberPicker label="共同編集者を追加する" disabled={busy} exclude={[creator.userId, ...list.map((p) => p.userId)]} onPick={(m) => apply([...list, { userId: m.id, displayName: m.displayName }], `${m.displayName}さんを共同編集者に追加しました。`)} />
      )}
    </section>
  );
}
