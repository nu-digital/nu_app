"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import ItemStatusBadges from "@/components/ItemStatus";
import MediaImg from "@/components/MediaImg";
import Notice from "@/components/Notice";
import Badge from "@/components/ui/Badge";
import Button from "@/components/ui/Button";
import { Loading } from "@/components/ui/States";
import { useToast } from "@/components/ui/Toast";
import { createLoan, describe, getItem, returnLoan, type ItemDetail } from "@/lib/api";
import { itemStatus } from "@/lib/item-status";

export default function ItemPage() {
  return <AppShell>{() => <Detail />}</AppShell>;
}

/** 数を増減するステッパー（−・入力・＋。スマホでも押しやすい44px）。 */
function Stepper({ id, label, value, min, max, onChange }: { id: string; label: string; value: number; min: number; max: number; onChange: (n: number) => void }) {
  const clamp = (n: number) => Math.max(min, Math.min(max, Number.isFinite(n) ? n : min));
  return (
    <div className="stepper" role="group" aria-label={`${label}の調整`}>
      <Button variant="default" fullWidth={false} aria-label={`${label}を減らす`} disabled={value <= min} onClick={() => onChange(clamp(value - 1))}>−</Button>
      <input id={id} className="input" type="number" min={min} max={max} inputMode="numeric" value={value} aria-label={label} onChange={(e) => onChange(clamp(Number(e.target.value)))} />
      <Button variant="default" fullWidth={false} aria-label={`${label}を増やす`} disabled={value >= max} onClick={() => onChange(clamp(value + 1))}>＋</Button>
    </div>
  );
}

function Gallery({ name, images }: { name: string; images: string[] }) {
  const [i, setI] = useState(0);
  const cur = Math.min(i, Math.max(0, images.length - 1));
  if (images.length === 0) return <div className="gallery"><div className="gallery-main empty" role="img" aria-label="写真は登録されていません"><span aria-hidden="true">{Array.from(name)[0]}</span></div></div>;
  return (
    <div className="gallery">
      <div className="gallery-main"><MediaImg id={images[cur]} alt={`${name}の写真（${cur + 1} / ${images.length}）`} /></div>
      {images.length > 1 && (
        <ul className="gallery-thumbs" aria-label="写真の切り替え">
          {images.map((m, k) => (
            <li key={m}><button type="button" aria-label={`写真${k + 1}を表示`} aria-current={k === cur} onClick={() => setI(k)}><MediaImg id={m} lazy /></button></li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Detail() {
  const { id } = useParams<{ id: string }>();
  const toast = useToast();
  const [it, setIt] = useState<ItemDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [qty, setQty] = useState(1);
  const [retQty, setRetQty] = useState<Record<string, number>>({});

  useEffect(() => {
    let alive = true;
    getItem(id)
      .then((v) => alive && setIt(v))
      .catch((e) => alive && setError((e as { status?: number }).status === 404 ? "この物品は見つかりません。" : describe(e)));
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
      toast("success", text);
    } catch (e) {
      setMsg(describe(e));
      try { await reload(); } catch { /* 表示の更新に失敗しても、エラーは上のメッセージで伝える */ }
    } finally {
      setBusy(false);
    }
  };

  if (error) return <main className="page"><Notice kind="error">{error}</Notice><p><Link href="/inventory">← 物品一覧へ</Link></p></main>;
  if (!it) return <main className="page"><Loading /></main>;

  const s = itemStatus(it);
  const max = it.mode === "quantity" ? it.available : 1;
  const myLoans = it.loans.filter((l) => l.mine);
  const otherLoans = it.loans.filter((l) => !l.mine);
  const qtyNow = Math.min(qty, Math.max(1, max));

  return (
    <main className="page wide item-detail">
      <p><Link href="/inventory">← 物品一覧へ</Link></p>
      <div className="detail-grid">
        <Gallery name={it.name} images={it.images} />
        <div className="detail-main">
          <p className="meta">{[it.category, it.location].filter(Boolean).join(" ・ ")}</p>
          <h1>{it.name}</h1>
          <ItemStatusBadges item={it} />
          {it.description && <p className="bio">{it.description}</p>}
          {msg && <Notice kind="error">{msg}</Notice>}

          <section className="borrow-box" aria-labelledby="stock-title">
            <h2 id="stock-title">{it.mode === "quantity" ? "借りる" : "個体を選んで借りる"}</h2>
            {it.mode === "quantity" ? (
              <>
                <p className="stock"><strong>残り {s.available}個</strong> / 全{s.total}個{it.outstanding > 0 ? `（貸出中 ${it.outstanding}個）` : ""}</p>
                {s.available > 0 ? (
                  <form className="borrow-form" onSubmit={(e) => { e.preventDefault(); void act(async () => { const r = await createLoan({ itemId: it.id, quantity: qtyNow }); setQty(1); return `${qtyNow}個借りました。残りは${r.remaining}個です。`; }); }}>
                    {max > 1 && <Stepper id="qty" label="借りる数" value={qtyNow} min={1} max={max} onChange={setQty} />}
                    <Button fullWidth={false} type="submit" disabled={busy}>{qtyNow}個借りる</Button>
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
                        <strong>{u.assetTag}</strong>{" "}
                        {u.mine ? <Badge tone="info">あなたが借りています</Badge> : u.loaned ? <Badge tone="danger">貸出中</Badge> : <Badge tone="success">借りられます</Badge>}
                        {u.loaned && !u.mine && <div className="user-id">{u.loaned.borrowerDisplayName}さん（{u.loaned.elapsedDays}日目）</div>}
                      </div>
                      {!u.loaned && <Button fullWidth={false} type="submit" disabled={busy} onClick={() => act(async () => { await createLoan({ itemId: it.id, unitId: u.id }); return `${u.assetTag} を借りました。`; })}>この個体を借りる</Button>}
                    </li>
                  ))}
                </ul>
              </>
            )}
          </section>
        </div>
      </div>

      {it.mode === "quantity" && it.loans.length > 0 && (
        <section className="panel" aria-labelledby="loans-title">
          <h2 id="loans-title">貸出中</h2>
          <p className="hint">返却は、借りた本人でなくても登録できます。</p>
          <ul className="items">
            {[...myLoans, ...otherLoans].map((l) => (
              <li key={l.id} className="item" data-loan={l.id}>
                <div><strong>{l.borrowerDisplayName}さん</strong> {l.quantity}個 {l.mine && <Badge tone="info">あなた</Badge>}<div className="user-id">{l.elapsedDays}日目</div></div>
                <div className="actions">
                  {l.quantity > 1 && <Stepper id={`rq-${l.id}`} label={`${l.borrowerDisplayName}さんの分の返却数`} value={retQty[l.id] ?? l.quantity} min={1} max={l.quantity} onChange={(n) => setRetQty({ ...retQty, [l.id]: n })} />}
                  <Button variant={l.mine ? "primary" : "default"} fullWidth={false} type="submit" disabled={busy} onClick={() => act(async () => { const n = retQty[l.id] ?? l.quantity; const r = await returnLoan(l.id, n); return `${n}個返却しました（未返却は${r.outstanding}個）。`; })}>返却する</Button>
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}
      {it.mode === "individual" && it.units.some((u) => u.loaned) && (
        <section className="panel">
          <h2>返却するには</h2>
          <p>貸出中の個体は、<Link href="/inventory/loans">貸出中の一覧</Link>から返却を登録できます（借りた本人でなくても登録できます）。</p>
        </section>
      )}
    </main>
  );
}
