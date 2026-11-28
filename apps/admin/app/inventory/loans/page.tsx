"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import { describe, listLoans, returnLoan, type OutstandingLoan } from "@/lib/api";

export default function LoansPage() {
  return <AppShell>{() => <Loans />}</AppShell>;
}

function Loans() {
  const [items, setItems] = useState<OutstandingLoan[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [mine, setMine] = useState(false);
  const [loading, setLoading] = useState(true);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [version, setVersion] = useState(0);
  const [retQty, setRetQty] = useState<Record<string, number>>({});

  useEffect(() => {
    let alive = true;
    listLoans({ mine })
      .then((r) => {
        if (!alive) return;
        setItems(r.items);
        setNext(r.nextCursor);
        setLoading(false);
      })
      .catch((e) => {
        if (!alive) return;
        setMsg({ kind: "error", text: describe(e) });
        setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [mine, version]);

  return (
    <main className="page">
      <p><Link href="/inventory">← 物品一覧へ</Link></p>
      <h1>貸出中の一覧</h1>
      <p className="lead">長く借りているものが上に出ます。返却は、借りた本人でなくても登録できます。</p>
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}
      <div className="field"><label><input type="checkbox" checked={mine} onChange={(e) => { setLoading(true); setMine(e.target.checked); }} /> 自分が借りているものだけ</label></div>
      {loading ? <p aria-busy="true">読み込んでいます…</p> : items.length === 0 ? <p>貸出中のものはありません。</p> : (
        <ul className="items">
          {items.map((l) => (
            <li key={l.id} className="item" data-loan={l.id}>
              <div>
                <strong>{l.itemName}</strong>{l.assetTag && `（${l.assetTag}）`} × {l.quantity}
                <div className="user-id">{l.borrowerDisplayName}さん{l.mine ? "（あなた）" : ""} ・ {l.elapsedDays}日目</div>
              </div>
              <div className="actions">
                {!l.assetTag && l.quantity > 1 && (
                  <>
                    <label className="visually-hidden" htmlFor={`lq-${l.id}`}>{l.itemName}の返却数</label>
                    <input id={`lq-${l.id}`} className="input narrow-input" type="number" min={1} max={l.quantity} inputMode="numeric" value={retQty[l.id] ?? l.quantity} onChange={(e) => setRetQty({ ...retQty, [l.id]: Math.max(1, Math.min(l.quantity, Number(e.target.value) || 1)) })} />
                  </>
                )}
                <Button variant="default" fullWidth={false} type="submit"
                  disabled={busyId === l.id}
                  onClick={async () => {
                    setBusyId(l.id);
                    setMsg(null);
                    try {
                      const n = !l.assetTag && l.quantity > 1 ? (retQty[l.id] ?? l.quantity) : l.quantity;
                      const r = await returnLoan(l.id, n);
                      setMsg({ kind: "success", text: `${l.itemName}を${r.returned}個返却しました。` });
                      setVersion((v) => v + 1);
                    } catch (e) {
                      setMsg({ kind: "error", text: describe(e) });
                    } finally {
                      setBusyId(null);
                    }
                  }}>
                  返却する
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {next && <Button variant="default" fullWidth={false} type="submit" onClick={async () => { try { const r = await listLoans({ mine, cursor: next }); setItems((c) => [...c, ...r.items]); setNext(r.nextCursor); } catch (e) { setMsg({ kind: "error", text: describe(e) }); } }}>さらに表示</Button>}
    </main>
  );
}
