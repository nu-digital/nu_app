"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import { describe, listHistory, type HistoryEvent } from "@/lib/api";

export default function HistoryPage() {
  return <AppShell>{() => <History />}</AppShell>;
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

function History() {
  const [items, setItems] = useState<HistoryEvent[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    let alive = true;
    listHistory()
      .then((r) => { if (!alive) return; setItems(r.items); setNext(r.nextCursor); setLoading(false); })
      .catch((e) => { if (!alive) return; setError(describe(e)); setLoading(false); });
    return () => { alive = false; };
  }, []);
  return (
    <main className="page">
      <p><Link href="/inventory">← 物品一覧へ</Link></p>
      <h1>貸出・返却の履歴</h1>
      {error && <Notice kind="error">{error}</Notice>}
      {loading ? <p aria-busy="true">読み込んでいます…</p> : items.length === 0 ? <p>履歴はまだありません。</p> : (
        <ul className="items">
          {items.map((e) => (
            <li key={`${e.kind}-${e.id}`} className="item" data-kind={e.kind}>
              <div>
                <span className={`badge ${e.kind === "loan" ? "status-invited" : e.kind === "return" ? "status-active" : "status-disabled"}`}>{KIND[e.kind]}</span>{" "}
                {describeEvent(e)}
                <div className="user-id">{new Date(e.at).toLocaleString("ja-JP")}{e.reason ? ` ・ 理由: ${e.reason}` : ""}</div>
              </div>
            </li>
          ))}
        </ul>
      )}
      {next && <button className="btn secondary inline" onClick={async () => { try { const r = await listHistory(next); setItems((c) => [...c, ...r.items]); setNext(r.nextCursor); } catch (e) { setError(describe(e)); } }}>さらに表示</button>}
    </main>
  );
}
