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
import Dialog from "@/components/ui/Dialog";
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
  // 返却ダイアログ（借りた人でなくても返せる。返却場所の初期値は現在の保管場所）
  const [ret, setRet] = useState<{ loanId: string; label: string; max: number; qty: number; location: string; proxy: boolean } | null>(null);

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
  const openReturn = (loanId: string, label: string, maxQty: number, proxy: boolean) => setRet({ loanId, label, max: maxQty, qty: maxQty, location: it.location, proxy });
  const submitReturn = () => {
    if (!ret) return;
    const r = ret;
    const place = r.location.trim();
    setRet(null);
    void act(async () => {
      const res = await returnLoan(r.loanId, r.max > 1 ? r.qty : undefined, place || undefined);
      const n = r.max > 1 ? `${r.qty}個` : "";
      return `${n}返却しました${place ? `（返却場所: ${place}）` : ""}${res.outstanding > 0 ? `。未返却は${res.outstanding}個です` : ""}。`;
    });
  };
  const mineTotal = s.mine;

  return (
    <main className="page wide item-detail">
      <p><Link href="/inventory">← 物品一覧へ</Link></p>
      <div className="detail-grid">
        <Gallery name={it.name} images={it.images} />
        <div className="detail-main">
          <p className="meta">{it.category}</p>
          <h1>{it.name}</h1>
          <ItemStatusBadges item={it} />
          <p className="location-line"><span className="label">保管場所</span> <strong data-testid="item-location">{it.location || "未設定"}</strong></p>
          {it.description && <p className="bio">{it.description}</p>}
          {msg && <Notice kind="error">{msg}</Notice>}

          <section className="borrow-box" aria-labelledby="stock-title">
            {it.mode === "quantity" ? (
              <>
                <h2 id="stock-title">{mineTotal > 0 ? (s.available > 0 ? "借りる・返す" : "返す") : s.available > 0 ? "借りる" : "貸出中"}</h2>
                <p className="stock"><strong>残り {s.available}個</strong> / 全{s.total}個{it.outstanding > 0 ? `（貸出中 ${it.outstanding}個）` : ""}</p>
                {s.available > 0 ? (
                  <form className="borrow-form" onSubmit={(e) => { e.preventDefault(); void act(async () => { const r = await createLoan({ itemId: it.id, quantity: qtyNow }); setQty(1); return `${qtyNow}個借りました。残りは${r.remaining}個です。`; }); }}>
                    {max > 1 && <Stepper id="qty" label="借りる数" value={qtyNow} min={1} max={max} onChange={setQty} />}
                    <Button fullWidth={false} type="submit" disabled={busy}>{mineTotal > 0 ? "追加で" : ""}{qtyNow}個借りる</Button>
                  </form>
                ) : mineTotal === 0 && <p className="hint">いま借りられる数がありません。返却されるまでお待ちください。</p>}
                {myLoans.length > 0 && (
                  <ul className="items my-loans" aria-label="あなたが借りている分">
                    {myLoans.map((l) => (
                      <li key={l.id} className="item" data-loan={l.id}>
                        <div><strong>あなたが借りている: {l.quantity}個</strong><div className="user-id">{l.elapsedDays}日目</div></div>
                        <Button variant="primary" fullWidth={false} disabled={busy} onClick={() => openReturn(l.id, `${l.quantity}個`, l.quantity, false)}>返す</Button>
                      </li>
                    ))}
                  </ul>
                )}
              </>
            ) : (
              <>
                <h2 id="stock-title">{it.units.some((u) => u.mine) ? "借りる・返す" : s.available > 0 ? "個体を選んで借りる" : "貸出中"}</h2>
                {it.units.length === 0 && <p className="hint">個体がまだ登録されていません。</p>}
                <ul className="items">
                  {it.units.filter((u) => u.state === "active").map((u) => (
                    <li key={u.id} className="item" data-tag={u.assetTag}>
                      <div>
                        <strong>{u.assetTag}</strong>{" "}
                        {u.mine ? <Badge tone="info">あなたが借りています</Badge> : u.loaned ? <Badge tone="danger">貸出中</Badge> : <Badge tone="success">借りられます</Badge>}
                        {u.loaned && !u.mine && <div className="user-id">{u.loaned.borrowerDisplayName}さん（{u.loaned.elapsedDays}日目）</div>}
                      </div>
                      {!u.loaned && <Button fullWidth={false} type="submit" disabled={busy} onClick={() => act(async () => { await createLoan({ itemId: it.id, unitId: u.id }); return `${u.assetTag} を借りました。`; })}>借りる</Button>}
                      {u.loaned && u.mine && u.loanId && <Button variant="primary" fullWidth={false} disabled={busy} onClick={() => openReturn(u.loanId!, u.assetTag, 1, false)}>返す</Button>}
                      {u.loaned && !u.mine && u.loanId && <Button variant="default" fullWidth={false} disabled={busy} onClick={() => openReturn(u.loanId!, `${u.assetTag}（${u.loaned!.borrowerDisplayName}さん）`, 1, true)}>代わりに返す</Button>}
                    </li>
                  ))}
                </ul>
              </>
            )}
          </section>
        </div>
      </div>

      {otherLoans.length > 0 && (
        <section className="panel" aria-labelledby="loans-title">
          <h2 id="loans-title">ほかのメンバーが借りています</h2>
          <p className="hint">借りた人に代わって返却を登録することもできます。</p>
          <ul className="items">
            {otherLoans.map((l) => (
              <li key={l.id} className="item" data-loan={l.id}>
                <div><strong>{l.borrowerDisplayName}さん</strong> {l.quantity}個<div className="user-id">{l.elapsedDays}日目</div></div>
                <Button variant="default" fullWidth={false} disabled={busy} onClick={() => openReturn(l.id, `${l.borrowerDisplayName}さんの${l.quantity}個`, l.quantity, true)}>代わりに返す</Button>
              </li>
            ))}
          </ul>
        </section>
      )}

      <Dialog
        open={ret !== null}
        title={ret?.proxy ? "代わりに返す" : "返す"}
        onClose={() => setRet(null)}
        actions={<Button fullWidth={false} disabled={busy} onClick={submitReturn}>返却する</Button>}
      >
        {ret && (
          <form className="return-form" onSubmit={(e) => { e.preventDefault(); submitReturn(); }}>
            <p><strong>{it.name}</strong>（{ret.label}）を返します。</p>
            {ret.max > 1 && <Stepper id="ret-qty" label="返す数" value={ret.qty} min={1} max={ret.max} onChange={(n) => setRet({ ...ret, qty: n })} />}
            <div className="field">
              <label htmlFor="ret-location">返却場所</label>
              <input id="ret-location" className="input" maxLength={50} value={ret.location} placeholder="例: 部室 棚A" onChange={(e) => setRet({ ...ret, location: e.target.value })} />
              <p className="hint">元の保管場所が入っています。別の場所に返すときは書き換えてください（物品の保管場所も更新されます）。</p>
            </div>
          </form>
        )}
      </Dialog>
    </main>
  );
}
