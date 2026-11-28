"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import {
  describe, listDeliveries, listNotificationSettings, putNotificationSetting, retryDelivery, testNotification,
  type Delivery, type NotificationSetting,
} from "@/lib/api";

export default function NotificationsPage() {
  return <AppShell>{() => <Notifications />}</AppShell>;
}

const STATUS: Record<Delivery["status"], { label: string; badge: string }> = {
  pending: { label: "送信待ち", badge: "status-invited" },
  sent: { label: "送信済み", badge: "status-active" },
  dead: { label: "失敗して停止", badge: "status-disabled" },
};

function Notifications() {
  const [settings, setSettings] = useState<NotificationSetting[]>([]);
  const [loading, setLoading] = useState(true);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);

  useEffect(() => {
    let alive = true;
    listNotificationSettings()
      .then((r) => { if (!alive) return; setSettings(r.items); setLoading(false); })
      .catch((e) => { if (!alive) return; setMsg({ kind: "error", text: describe(e) }); setLoading(false); });
    return () => { alive = false; };
  }, []);

  return (
    <main className="page wide">
      <p><Link href="/admin/inventory">← 物品の管理へ</Link></p>
      <h1>通知の設定</h1>
      <p className="lead">物品の貸出・返却・訂正が記録されたとき、Discordのチャンネルへお知らせします。送信に失敗しても、貸出・返却の記録は消えません（失敗は下の「送信の状況」に残ります）。</p>
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}
      {loading ? <p aria-busy="true">読み込んでいます…</p> : (
        <ul className="items settings">
          {settings.map((s) => (
            <SettingRow key={s.eventType} s={s} onMsg={setMsg} onSaved={(n) => setSettings((cur) => cur.map((x) => (x.eventType === n.eventType ? n : x)))} />
          ))}
        </ul>
      )}
      <Deliveries onMsg={setMsg} />
    </main>
  );
}

function SettingRow({ s, onSaved, onMsg }: { s: NotificationSetting; onSaved: (n: NotificationSetting) => void; onMsg: (m: { kind: "error" | "success"; text: string } | null) => void }) {
  const [enabled, setEnabled] = useState(s.enabled);
  const [channel, setChannel] = useState(s.discordChannelId ?? "");
  const [busy, setBusy] = useState("");
  const dirty = enabled !== s.enabled || channel.trim() !== (s.discordChannelId ?? "");
  const run = async (kind: string, f: () => Promise<string>) => {
    if (busy) return;
    setBusy(kind);
    onMsg(null);
    try {
      onMsg({ kind: "success", text: await f() });
    } catch (e) {
      onMsg({ kind: "error", text: describe(e) });
    } finally {
      setBusy("");
    }
  };
  const id = s.eventType.replace(/[^a-z]/g, "-");
  return (
    <li className="item setting" data-event={s.eventType}>
      <section aria-labelledby={`h-${id}`} className="setting-body">
        <h2 id={`h-${id}`}>{s.label}</h2>
        <div className="field">
          <label><input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} /> この通知を送る</label>
        </div>
        <div className="field">
          <label htmlFor={`ch-${id}`}>送信先のチャンネルID</label>
          <input id={`ch-${id}`} className="input" inputMode="numeric" autoComplete="off" placeholder="例: 123456789012345678" value={channel} onChange={(e) => setChannel(e.target.value)} />
          <p className="hint">Discordの設定で「開発者モード」をオンにし、チャンネルを右クリック →「チャンネルIDをコピー」。</p>
        </div>
        {enabled && !(s.discordChannelId ?? "") && <p className="hint">チャンネルが未設定のあいだは送らず、送信待ちとして溜まります（設定すると送られます）。</p>}
        <div className="actions">
          <Button fullWidth={false} type="submit" disabled={!dirty || !!busy} onClick={() => run("save", async () => { const n = await putNotificationSetting(s.eventType, enabled, channel.trim()); onSaved(n); setChannel(n.discordChannelId ?? ""); return `「${s.label}」の設定を保存しました。`; })}>保存する</Button>
          <Button variant="default" fullWidth={false} type="submit" disabled={!channel.trim() || !!busy} onClick={() => run("test", async () => { await testNotification(channel.trim()); return "テスト通知を送りました。Discordのチャンネルを確認してください。"; })}>接続テスト</Button>
        </div>
      </section>
    </li>
  );
}

function Deliveries({ onMsg }: { onMsg: (m: { kind: "error" | "success"; text: string } | null) => void }) {
  const [status, setStatus] = useState("");
  const [items, setItems] = useState<Delivery[]>([]);
  const [loading, setLoading] = useState(true);
  const [version, setVersion] = useState(0);
  const [busyId, setBusyId] = useState("");

  useEffect(() => {
    let alive = true;
    listDeliveries(status)
      .then((r) => { if (!alive) return; setItems(r.items); setLoading(false); })
      .catch((e) => { if (!alive) return; onMsg({ kind: "error", text: describe(e) }); setLoading(false); });
    return () => { alive = false; };
  }, [status, version, onMsg]);

  return (
    <section aria-labelledby="deliveries-title">
      <h2 id="deliveries-title">送信の状況</h2>
      <div className="filters">
        <div className="field">
          <label htmlFor="d-status">表示する状態</label>
          <select id="d-status" className="input" value={status} onChange={(e) => { setLoading(true); setStatus(e.target.value); }}>
            <option value="">すべて</option>
            <option value="pending">送信待ち</option>
            <option value="dead">失敗して停止</option>
            <option value="sent">送信済み</option>
          </select>
        </div>
        <Button variant="default" fullWidth={false} type="submit" onClick={() => { setLoading(true); setVersion((v) => v + 1); }}>更新する</Button>
      </div>
      {loading ? <p aria-busy="true">読み込んでいます…</p> : items.length === 0 ? <p>該当する通知はありません。</p> : (
        <ul className="items">
          {items.map((d) => (
            <li key={d.id} className="item" data-delivery={d.id} data-status={d.status}>
              <div>
                <span className={`badge ${STATUS[d.status].badge}`}>{STATUS[d.status].label}</span> <strong>{d.label}</strong>
                <div className="user-id">{new Date(d.createdAt).toLocaleString("ja-JP")} ・ 試行{d.attempts}回</div>
                {d.lastError && <div className="user-id">直近の失敗: {d.lastError}</div>}
              </div>
              {d.status === "dead" && (
                <Button variant="default" fullWidth={false} type="submit"
                  disabled={busyId === d.id}
                  onClick={async () => {
                    setBusyId(d.id);
                    onMsg(null);
                    try {
                      await retryDelivery(d.id);
                      onMsg({ kind: "success", text: "再送待ちに戻しました。まもなく送信されます。" });
                      setLoading(true);
                      setVersion((v) => v + 1);
                    } catch (e) {
                      onMsg({ kind: "error", text: describe(e) });
                    } finally {
                      setBusyId("");
                    }
                  }}>
                  再送する
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
