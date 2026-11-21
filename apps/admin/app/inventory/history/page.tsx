"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import { createCorrection, describe, listHistory, type HistoryEvent } from "@/lib/api";

export default function HistoryPage() {
  return <AppShell>{(me) => <History canCorrect={me.user.roles.includes("admin") || me.user.roles.includes("owner")} />}</AppShell>;
}

const KIND: Record<HistoryEvent["kind"], string> = { loan: "貸出", return: "返却", correction: "訂正" };

function describeEvent(e: HistoryEvent): string {
  const what = `${e.itemName}${e.assetTag ? `（${e.assetTag}）` : ""}`;
  if (e.kind === "loan") return `${e.borrowerDisplayName}さんが${what}を${e.quantity}個借りました`;
  if (e.kind === "return") {
    const proxy = e.actorDisplayName !== e.borrowerDisplayName;
    return `${what}を${e.quantity}個返却しました（借りた人: ${e.borrowerDisplayName}さん${proxy ? `／返却を登録した人: ${e.actorDisplayName}さん` : ""}）`;
  }
  const target = e.targetType === "return" ? "返却" : "貸出";
  const action = e.correctionKind === "void" ? `${target}の記録を取り消しました` : `${target}の数量を${e.quantity > 0 ? `+${e.quantity}` : e.quantity}個訂正しました`;
  return `${e.actorDisplayName}さんが${what}の${action}（借りた人: ${e.borrowerDisplayName}さん）`;
}

function History({ canCorrect }: { canCorrect: boolean }) {
  const [items, setItems] = useState<HistoryEvent[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [version, setVersion] = useState(0);
  useEffect(() => {
    let alive = true;
    listHistory()
      .then((r) => { if (!alive) return; setItems(r.items); setNext(r.nextCursor); setLoading(false); })
      .catch((e) => { if (!alive) return; setError(describe(e)); setLoading(false); });
    return () => { alive = false; };
  }, [version]);
  return (
    <main className="page">
      <p><Link href="/inventory">← 物品一覧へ</Link></p>
      <h1>貸出・返却の履歴</h1>
      {error && <Notice kind="error">{error}</Notice>}
      {notice && <Notice kind="success">{notice}</Notice>}
      {loading ? <p aria-busy="true">読み込んでいます…</p> : items.length === 0 ? <p>履歴はまだありません。</p> : (
        <ul className="items">
          {items.map((e) => (
            <li key={`${e.kind}-${e.id}`} className="item" data-kind={e.kind}>
              <div>
                <span className={`badge ${e.kind === "loan" ? "status-invited" : e.kind === "return" ? "status-active" : "status-disabled"}`}>{KIND[e.kind]}</span>{" "}
                {describeEvent(e)}
                <div className="user-id">{new Date(e.at).toLocaleString("ja-JP")}{e.reason ? ` ・ 理由: ${e.reason}` : ""}</div>
                {canCorrect && e.kind !== "correction" && (
                  <CorrectionForm ev={e} onDone={(t) => { setNotice(t); setError(""); setLoading(true); setVersion((v) => v + 1); }} onError={setError} />
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {next && <button className="btn secondary inline" onClick={async () => { try { const r = await listHistory(next); setItems((c) => [...c, ...r.items]); setNext(r.nextCursor); } catch (e) { setError(describe(e)); } }}>さらに表示</button>}
    </main>
  );
}

// 管理者だけの訂正。元の記録は消さず、訂正が履歴に追加される（D09）。フォームを開くたびに新しい冪等キーを作る。
function CorrectionForm({ ev, onDone, onError }: { ev: HistoryEvent; onDone: (text: string) => void; onError: (text: string) => void }) {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<"void" | "adjust">("void");
  const [delta, setDelta] = useState(1);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [key, setKey] = useState("");
  const individual = ev.assetTag !== "";
  if (!open) {
    return (
      <button className="btn secondary inline" onClick={() => { setKey(`ui-${crypto.randomUUID()}`); setOpen(true); }}>
        この記録を訂正する
      </button>
    );
  }
  return (
    <form
      className="panel correction"
      aria-label="記録の訂正"
      onSubmit={async (x) => {
        x.preventDefault();
        if (busy) return;
        setBusy(true);
        try {
          await createCorrection(key, { targetType: ev.kind as "loan" | "return", targetId: ev.id, kind, ...(kind === "adjust" ? { deltaQuantity: delta } : {}), reason: reason.trim() });
          onDone(kind === "void" ? "記録を取り消しました（元の記録は履歴に残ります）。" : "数量を訂正しました（元の記録は履歴に残ります）。");
        } catch (err) {
          onError(describe(err));
          setBusy(false);
        }
      }}
    >
      <p className="hint">元の記録は消えず、訂正が履歴に追加されます。理由は管理者にだけ表示されます。</p>
      <div className="field">
        <label htmlFor={`ck-${ev.kind}-${ev.id}`}>訂正の種類</label>
        <select id={`ck-${ev.kind}-${ev.id}`} className="input" value={kind} onChange={(x) => setKind(x.target.value as "void" | "adjust")}>
          <option value="void">この記録を取り消す</option>
          {!individual && <option value="adjust">数量を増減する</option>}
        </select>
      </div>
      {kind === "adjust" && (
        <div className="field">
          <label htmlFor={`cd-${ev.kind}-${ev.id}`}>増減する数（減らすときはマイナス）</label>
          <input id={`cd-${ev.kind}-${ev.id}`} className="input" type="number" inputMode="numeric" value={delta} onChange={(x) => setDelta(Number(x.target.value) || 0)} />
        </div>
      )}
      <div className="field">
        <label htmlFor={`cr-${ev.kind}-${ev.id}`}>訂正の理由（必須）</label>
        <input id={`cr-${ev.kind}-${ev.id}`} className="input" maxLength={200} value={reason} onChange={(x) => setReason(x.target.value)} />
      </div>
      <div className="actions">
        <button className="btn inline" type="submit" disabled={busy || !reason.trim() || (kind === "adjust" && delta === 0)}>訂正する</button>
        <button className="btn secondary inline" type="button" onClick={() => setOpen(false)} disabled={busy}>やめる</button>
      </div>
    </form>
  );
}
