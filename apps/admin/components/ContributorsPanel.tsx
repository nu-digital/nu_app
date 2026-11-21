"use client";

import { useState } from "react";

import MemberPicker from "@/components/MemberPicker";
import Notice from "@/components/Notice";
import { conflictRevision, describe, putContributors, type ContentDetail } from "@/lib/api";

type Row = { userId: string; displayName: string; roleLabel: string };

/**
 * 制作メンバー（作品）。誰が・どの役割で関わったかを登録する。**公開サイトに名前が出るのは、本人が「インターネット公開（公開クレジット）」に
 * 同意しているメンバーだけ**で、同意していない人・取り消した人は「匿名のメンバー」と表示される（同意は本人だけが変更できる）。
 * 制作メンバーは版ごとに持つため、保存すると新しい版ができ、公開版は再公開するまで変わらない。
 */
export default function ContributorsPanel({ doc, dirty, onSaved, onConflict }: { doc: ContentDetail; dirty: boolean; onSaved: (c: ContentDetail) => void; onConflict: () => void }) {
  const initial: Row[] = (doc.workingRevision.contributors ?? []).map((c) => ({ userId: c.userId, displayName: c.displayName, roleLabel: c.roleLabel }));
  const [rows, setRows] = useState<Row[]>(initial);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const changed = JSON.stringify(rows.map((r) => [r.userId, r.roleLabel.trim()])) !== JSON.stringify(initial.map((r) => [r.userId, r.roleLabel]));
  const valid = rows.every((r) => r.roleLabel.trim() !== "");

  async function save() {
    if (busy) return;
    setBusy(true);
    setMsg(null);
    try {
      const c = await putContributors(doc.id, doc.workingRevision.revisionNo, rows.map((r) => ({ userId: r.userId, roleLabel: r.roleLabel.trim() })));
      onSaved(c); // 保存すると版が変わりパネルが作り直されるため、成功の案内は呼び出し側（編集画面）が出す
    } catch (e) {
      if (conflictRevision(e)) {
        setMsg({ kind: "error", text: "他の人が先に保存しました。最新を読み込み直します。" });
        onConflict();
      } else {
        setMsg({ kind: "error", text: describe(e) });
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel" aria-labelledby="contrib-title">
      <h2 id="contrib-title">制作メンバー</h2>
      <p className="hint">
        この作品に関わった人と役割を登録します。<strong>公開サイトに名前が載るのは、本人が「インターネットへの公開（公開クレジット）」に同意している人だけ</strong>です。同意していない人・取り消した人は「匿名のメンバー」と表示されます（役割は出ます）。同意は本人だけが、プロフィールで変えられます。
      </p>
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}
      {rows.length === 0 ? <p>制作メンバーはまだ登録されていません。</p> : (
        <ul className="items" data-testid="contributors">
          {rows.map((r, i) => (
            <li key={`${r.userId}-${i}`} className="item" data-person={r.displayName}>
              <div className="grow">
                <strong>{r.displayName}</strong>
                <div className="field">
                  <label htmlFor={`role-${i}`}>{r.displayName}さんの役割</label>
                  <input id={`role-${i}`} className="input" maxLength={50} value={r.roleLabel} onChange={(e) => setRows(rows.map((x, j) => (j === i ? { ...x, roleLabel: e.target.value } : x)))} />
                </div>
              </div>
              <button className="btn secondary inline" disabled={busy} onClick={() => setRows(rows.filter((_, j) => j !== i))}>{r.displayName}さんを外す</button>
            </li>
          ))}
        </ul>
      )}
      <MemberPicker label="制作メンバーを追加する" disabled={busy} exclude={[]} onPick={(m) => setRows([...rows, { userId: m.id, displayName: m.displayName, roleLabel: "" }])} />
      <div className="actions">
        <button className="btn inline" onClick={save} disabled={busy || !changed || !valid || dirty}>制作メンバーを保存する</button>
        {dirty ? <span className="hint">先に、本文などの変更を保存してください（制作メンバーの保存は、新しい版を作ります）。</span> : !valid ? <span className="hint">すべての人に役割を入れてください。</span> : null}
      </div>
    </section>
  );
}
