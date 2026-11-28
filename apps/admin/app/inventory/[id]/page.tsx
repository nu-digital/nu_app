"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import MediaImg from "@/components/MediaImg";
import Button from "@/components/ui/Button";
import { createLoan, describe, getItem, returnLoan, type ItemDetail } from "@/lib/api";

export default function ItemPage() {
  return <AppShell>{() => <Detail />}</AppShell>;
}

function Detail() {
  const { id } = useParams<{ id: string }>();
  const [it, setIt] = useState<ItemDetail | null>(null);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [qty, setQty] = useState(1);
  const [retQty, setRetQty] = useState<Record<string, number>>({});

  useEffect(() => {
    let alive = true;
    getItem(id)
      .then((v) => alive && setIt(v))
      .catch((e) => alive && setMsg({ kind: "error", text: (e as { status?: number }).status === 404 ? "この物品は見つかりません。" : describe(e) }));
    return () => {
      alive = false;
    };
  }, [id]);

  const reload = useCallback(async () => setIt(await getItem(id)), [id]);
  const act = async (f: () => Promise<string>) => {
    if (busy) return;
    setBusy(true);
    setMsg(null);
    try {
      const text = await f();
      await reload();
      setMsg({ kind: "success", text });
    } catch (e) {
      setMsg({ kind: "error", text: describe(e) });
      try { await reload(); } catch { /* 表示の更新に失敗しても、エラーは上のメッセージで伝える */ }
    } finally {
      setBusy(false);
    }
  };

  if (!it) return <main className="page" aria-busy="true">{msg && <Notice kind={msg.kind}>{msg.text}</Notice>}<p><Link href="/inventory">← 物品一覧へ</Link></p></main>;

  const max = it.mode === "quantity" ? it.available : 1;
  return (
    <main className="page">
      <p><Link href="/inventory">← 物品一覧へ</Link></p>
      <h1>{it.name}</h1>
      {it.photoMediaId && <MediaImg className="photo" id={it.photoMediaId} alt={`${it.name}の写真`} />}
      <p className="meta">{[it.category, it.location].filter(Boolean).join(" ・ ")}</p>
      {it.description && <p className="bio">{it.description}</p>}
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}

      <section className="panel" aria-labelledby="stock-title">
        <h2 id="stock-title">{it.mode === "quantity" ? "貸出の状況" : "個体の状況"}</h2>
        {it.mode === "quantity" ? (
          <>
            <p className="stock"><strong>残り {it.available}</strong> / 全{it.totalQuantity}（貸出中 {it.outstanding}）</p>
            {it.available > 0 ? (
              <form onSubmit={(e) => { e.preventDefault(); void act(async () => { const r = await createLoan({ itemId: it.id, quantity: qty }); setQty(1); return `${qty}個借りました。残りは${r.remaining}個です。`; }); }}>
                <div className="field">
                  <label htmlFor="qty">借りる数</label>
                  <input id="qty" className="input" type="number" min={1} max={max} inputMode="numeric" value={qty} onChange={(e) => setQty(Math.max(1, Math.min(max, Number(e.target.value) || 1)))} />
                </div>
                <Button fullWidth={false} type="submit" disabled={busy}>借りる</Button>
              </form>
            ) : (
              <p className="hint">いま借りられる数がありません。返却されるまでお待ちください。</p>
            )}
          </>
        ) : (
          <>
            {it.units.length === 0 && <p className="hint">個体がまだ登録されていません。</p>}
            <ul className="items">
              {it.units.filter((u) => u.state === "active").map((u) => (
                <li key={u.id} className="item" data-tag={u.assetTag}>
                  <div>
                    <strong>{u.assetTag}</strong>
                    <div className="user-id">{u.loaned ? `貸出中: ${u.loaned.borrowerDisplayName}さん（${u.loaned.elapsedDays}日目）` : "借りられます"}</div>
                  </div>
                  {!u.loaned && <Button fullWidth={false} type="submit" disabled={busy} onClick={() => act(async () => { await createLoan({ itemId: it.id, unitId: u.id }); return `${u.assetTag} を借りました。`; })}>この個体を借りる</Button>}
                </li>
              ))}
            </ul>
          </>
        )}
      </section>

      {it.mode === "quantity" && (
        <section className="panel" aria-labelledby="loans-title">
          <h2 id="loans-title">貸出中</h2>
          <p className="hint">返却は、借りた本人でなくても登録できます。</p>
          {it.loans.length === 0 ? <p>貸出中のものはありません。</p> : (
            <ul className="items">
              {it.loans.map((l) => (
                <li key={l.id} className="item" data-loan={l.id}>
                  <div><strong>{l.borrowerDisplayName}さん</strong> {l.quantity}個<div className="user-id">{l.elapsedDays}日目</div></div>
                  <div className="actions">
                    <label className="visually-hidden" htmlFor={`rq-${l.id}`}>{l.borrowerDisplayName}さんの分の返却数</label>
                    <input id={`rq-${l.id}`} className="input narrow-input" type="number" min={1} max={l.quantity} inputMode="numeric" value={retQty[l.id] ?? l.quantity} onChange={(e) => setRetQty({ ...retQty, [l.id]: Math.max(1, Math.min(l.quantity, Number(e.target.value) || 1)) })} />
                    <Button variant="default" fullWidth={false} type="submit" disabled={busy} onClick={() => act(async () => { const n = retQty[l.id] ?? l.quantity; const r = await returnLoan(l.id, n); return `${n}個返却しました（未返却は${r.outstanding}個）。`; })}>返却する</Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
      {it.mode === "individual" && it.units.some((u) => u.loaned) && (
        <ReturnUnits />
      )}
    </main>
  );
}

// 個体管理の返却は、貸出中の一覧（/inventory/loans）から行う。ここでは案内だけ。
function ReturnUnits() {
  return (
    <section className="panel">
      <h2>返却するには</h2>
      <p>貸出中の個体は、<Link href="/inventory/loans">貸出中の一覧</Link>から返却を登録できます（借りた本人でなくても登録できます）。</p>
    </section>
  );
}
