"use client";

import { useMemo, useState } from "react";

import MemberPicker from "@/components/MemberPicker";
import Notice from "@/components/Notice";
import Badge from "@/components/ui/Badge";
import Button from "@/components/ui/Button";
import { Avatar } from "@/components/ui/Avatar";
import { useToast } from "@/components/ui/Toast";
import { conflictRevision, describe, putCollaborators, putContributors, type ContentDetail } from "@/lib/api";

/**
 * 記事・作品のメンバー（共同編集者と制作メンバーを1つにまとめた設定）。
 *  - メンバーに追加した人は、この記事・作品を**編集できる**（公開はできない）。
 *  - 作品では、役割を入れた人が制作メンバーとして公式サイトにクレジットされる（役割が空ならクレジットしない）。
 *    **名前が公式サイトに出るのは、本人が「インターネットへの公開」に同意している場合だけ**（同意は本人だけが変えられる）。
 * 既存の人の権限・クレジットは、触らない限り変わらない（保存では差分だけを送る）。
 * 内部では、編集権限（共同編集者。記事単位・すぐ反映）と制作クレジット（版ごと・保存すると新しい版）を別々に保存している。
 */
type Row = { userId: string; displayName: string; avatarMediaId: string | null; creator: boolean; edit: boolean; role: string };

function initialRows(doc: ContentDetail): Row[] {
  const rows: Row[] = [{ ...doc.creator, creator: true, edit: true, role: "" }];
  for (const c of doc.collaborators) if (!rows.some((r) => r.userId === c.userId)) rows.push({ ...c, creator: false, edit: true, role: "" });
  for (const c of doc.workingRevision.contributors ?? []) {
    const r = rows.find((x) => x.userId === c.userId);
    if (r) r.role = c.roleLabel;
    else rows.push({ userId: c.userId, displayName: c.displayName, avatarMediaId: c.avatarMediaId, creator: false, edit: false, role: c.roleLabel });
  }
  return rows;
}

export default function MembersPanel({ doc, ensureSaved, onSaved, onConflict }: { doc: ContentDetail; ensureSaved: () => Promise<ContentDetail | null>; onSaved: (c: ContentDetail) => void; onConflict: () => void }) {
  const toast = useToast();
  const isProject = doc.type === "project";
  const canManage = doc.canManageCollaborators;
  const initial = useMemo(() => initialRows(doc), [doc]);
  const [rows, setRows] = useState<Row[]>(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const targetCollab = rows.filter((r) => r.edit && !r.creator).map((r) => r.userId);
  const curCollab = doc.collaborators.map((c) => c.userId);
  const collabChanged = canManage && JSON.stringify([...targetCollab].sort()) !== JSON.stringify([...curCollab].sort());
  const targetContrib = rows.filter((r) => r.role.trim() !== "").map((r) => ({ userId: r.userId, roleLabel: r.role.trim() }));
  const curContrib = (doc.workingRevision.contributors ?? []).map((c) => ({ userId: c.userId, roleLabel: c.roleLabel }));
  const contribChanged = isProject && JSON.stringify(targetContrib) !== JSON.stringify(curContrib);
  const changed = collabChanged || contribChanged;

  async function save() {
    if (busy || !changed) return;
    setBusy(true);
    setError("");
    try {
      let latest = doc;
      if (collabChanged) {
        const r = await putCollaborators(doc.id, targetCollab);
        latest = { ...latest, collaborators: r.collaborators };
      }
      if (contribChanged) {
        // 制作メンバーの保存は新しい版を作る。編集中の本文があれば、先に保存してから行う
        const base = await ensureSaved();
        if (!base) { setBusy(false); return; }
        latest = await putContributors(doc.id, base.workingRevision.revisionNo, targetContrib);
      }
      toast("success", "メンバーを保存しました。");
      onSaved(latest);
    } catch (e) {
      if (conflictRevision(e)) {
        setError("他の人が先に保存しました。最新を読み込み直します。");
        onConflict();
      } else {
        setError(describe(e));
      }
    } finally {
      setBusy(false);
    }
  }

  const add = (m: { id: string; displayName: string; avatarMediaId: string | null }) =>
    setRows((cur) => (cur.some((r) => r.userId === m.id) ? cur : [...cur, { userId: m.id, displayName: m.displayName, avatarMediaId: m.avatarMediaId, creator: false, edit: canManage, role: isProject ? "制作" : "" }]));
  const mayAdd = canManage || isProject;

  return (
    <section className="panel" aria-labelledby="members-title">
      <h2 id="members-title">メンバー</h2>
      <p className="hint">
        ここに追加した人は、この{isProject ? "作品" : "記事"}を<strong>編集できます</strong>（公開はできません）。
        {isProject && <> 役割を入れた人は、制作メンバーとして公式サイトにクレジットされます（空ならクレジットしません）。<strong>名前が出るのは、本人が「インターネットへの公開」に同意している場合だけ</strong>です。</>}
        {!canManage && <> メンバーの追加・削除ができるのは、作者と編集者（Editor）以上です。</>}
      </p>
      {error && <Notice kind="error">{error}</Notice>}
      <ul className="member-rows" data-testid="members">
        {rows.map((r) => (
          <li key={r.userId} className="member-row-card" data-person={r.displayName}>
            <Avatar name={r.displayName} mediaId={r.avatarMediaId} size={40} />
            <div className="grow">
              <strong>{r.displayName}</strong>{" "}
              {r.creator ? <Badge tone="info" icon="★">作者</Badge> : r.edit ? <Badge tone="neutral" icon="✎">編集できる</Badge> : <Badge tone="neutral" icon="・">クレジットのみ</Badge>}
              {isProject && (
                <div className="field">
                  <label htmlFor={`role-${r.userId}`}>{r.displayName}さんの役割（公式サイトのクレジット）</label>
                  <input id={`role-${r.userId}`} className="input" maxLength={50} placeholder="例: 監督・モデリング（空ならクレジットしない）" value={r.role} onChange={(e) => setRows((cur) => cur.map((x) => (x.userId === r.userId ? { ...x, role: e.target.value } : x)))} />
                </div>
              )}
            </div>
            {!r.creator && (canManage || (isProject && !r.edit)) && (
              <Button variant="default" fullWidth={false} disabled={busy} aria-label={`${r.displayName}さんを外す`} onClick={() => setRows((cur) => cur.filter((x) => x.userId !== r.userId))}>外す</Button>
            )}
          </li>
        ))}
      </ul>
      {mayAdd && <MemberPicker label="メンバーを追加する" disabled={busy} exclude={rows.map((r) => r.userId)} onPick={add} />}
      <div className="actions">
        <Button fullWidth={false} onClick={save} disabled={busy || !changed}>{busy ? "保存しています…" : "メンバーを保存する"}</Button>
        {changed && <span className="hint">保存していない変更があります{contribChanged ? "（制作メンバーを保存すると、編集中の本文も一緒に下書き保存されます）" : ""}</span>}
      </div>
    </section>
  );
}
