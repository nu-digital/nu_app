"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";

import AppShell from "@/components/AppShell";
import AvatarEditor from "@/components/AvatarEditor";
import FieldEditor, { VIS_LABEL, type FieldSpec } from "@/components/FieldEditor";
import Notice from "@/components/Notice";
import Badge from "@/components/ui/Badge";
import Button from "@/components/ui/Button";
import Dialog from "@/components/ui/Dialog";
import Select from "@/components/ui/Select";
import TextField from "@/components/ui/TextField";
import Textarea from "@/components/ui/Textarea";
import { Avatar } from "@/components/ui/Avatar";
import SegmentedControl from "@/components/ui/SegmentedControl";
import { Loading } from "@/components/ui/States";
import { useToast } from "@/components/ui/Toast";
import {
  addDevice, addLink, deleteDevice, deleteLink, describe, getMyProfile, patchDevice, patchLink, patchMyProfile, putConsent, putVisibility,
  type MyProfile, type Visibility,
} from "@/lib/api";

export default function ProfilePage() {
  return <AppShell>{(me) => <Profile loginId={me.user.loginId} />}</AppShell>;
}

type Tab = "profile" | "visibility" | "private" | "account";

/** 設定の1行（BOOTHのアカウント設定のように: 項目名・現在の内容・公開範囲・変更）。 */
function Row({ name, value, vis, empty = "未設定", onEdit, editLabel, children }: { name: string; value?: ReactNode; vis?: Visibility | "fixed-members" | "fixed-private"; empty?: string; onEdit?: () => void; editLabel?: string; children?: ReactNode }) {
  const isEmpty = value === undefined || value === null || value === "";
  return (
    <li className="setting-row" data-setting={name}>
      <div className="s-name">{name}</div>
      <div className="s-value">{isEmpty ? <span className="s-empty">{empty}</span> : value}{children}</div>
      <div className="s-vis">
        {vis === "members" || vis === "fixed-members" ? <Badge tone="info" icon="◎">メンバーに公開</Badge> : vis === "private" || vis === "fixed-private" ? <Badge tone="neutral" icon="●">非公開</Badge> : null}
      </div>
      <div className="s-edit">{onEdit && <Button variant="default" fullWidth={false} onClick={onEdit} aria-label={`${name}を${editLabel ?? "変更"}`}>{editLabel ?? "変更"}</Button>}</div>
    </li>
  );
}

function Profile({ loginId }: { loginId: string }) {
  const toast = useToast();
  const [p, setP] = useState<MyProfile | null>(null);
  const [error, setError] = useState("");
  const [tab, setTab] = useState<Tab>("profile");
  const [spec, setSpec] = useState<FieldSpec | null>(null);
  const [avatarOpen, setAvatarOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => { setP(await getMyProfile()); }, []);
  useEffect(() => {
    let alive = true;
    getMyProfile().then((v) => alive && setP(v)).catch((e) => alive && setError(describe(e)));
    return () => { alive = false; };
  }, []);

  /** 保存して読み込み直し、完了をトーストで伝える。失敗は呼び出し側（ダイアログ）に投げる。 */
  const save = (f: () => Promise<unknown>, done: string) => async () => { await f(); await reload(); toast("success", done); };
  const act = async (f: () => Promise<unknown>, done: string) => {
    if (busy) return;
    setBusy(true);
    setError("");
    try { await save(f, done)(); } catch (e) { setError(describe(e)); } finally { setBusy(false); }
  };

  if (!p) return <main className="page">{error ? <Notice kind="error">{error}</Notice> : <Loading />}</main>;

  const vis = p.visibility;
  const field = (title: string, label: string, value: string, key: "bio" | "faculty" | "department", extra: Partial<FieldSpec> = {}): FieldSpec => ({
    title, label, value, visibility: vis[key], ...extra,
    onSave: async (v, nv) => { await patchMyProfile({ [key]: v }); if (nv && nv !== vis[key]) await putVisibility({ ...vis, [key]: nv }); await reload(); toast("success", `${title.replace("を変更", "")}を保存しました。`); },
  });
  const privateField = (title: string, label: string, key: "legalName" | "universityEmail" | "studentNumber" | "nearestStation", extra: Partial<FieldSpec> = {}): FieldSpec => ({
    title, label, value: p.private[key] ?? "", ...extra,
    onSave: async (v) => { await patchMyProfile({ [key]: v }); await reload(); toast("success", `${label}を保存しました。`); },
  });

  return (
    <main className="page">
      <h1>アカウント設定</h1>
      <p className="lead">現在の設定を確認して、変えたい項目だけ「変更」から編集できます。初期設定では、表示名とアイコン以外はすべて非公開です。</p>
      {error && <Notice kind="error">{error}</Notice>}
      <div className="settings-tabs"><SegmentedControl label="設定の種類" value={tab} onChange={setTab} options={[{ value: "profile", label: "プロフィール" }, { value: "visibility", label: "公開設定" }, { value: "private", label: "個人情報" }, { value: "account", label: "アカウント" }]} /></div>

      {tab === "profile" && (
        <>
          <section className="panel" aria-labelledby="s-basic"><h2 id="s-basic">基本情報</h2>
            <ul className="settings">
              <Row name="アイコン" vis="fixed-members" empty="未設定" onEdit={() => setAvatarOpen(true)} value={p.avatarMediaId ? <Avatar name={p.displayName} mediaId={p.avatarMediaId} size={48} /> : undefined} />
              <Row name="表示名" vis="fixed-members" value={p.displayName} onEdit={() => setSpec({ title: "表示名を変更", label: "表示名（必須）", value: p.displayName, maxLength: 64, required: true, hint: "ログイン中のメンバーに表示されます（64文字まで）。", onSave: async (v) => { await patchMyProfile({ displayName: v }); await reload(); toast("success", "表示名を保存しました。"); } })} />
              <Row name="ぬでじID" vis="fixed-private" value={loginId}><span className="hint s-note">ログインに使うIDです。変更は管理者に依頼してください。</span></Row>
              <Row name="自己紹介" vis={vis.bio} value={p.bio ? <span className="bio">{p.bio}</span> : undefined} onEdit={() => setSpec(field("自己紹介を変更", "自己紹介", p.bio ?? "", "bio", { kind: "textarea", maxLength: 1000 }))} />
              <Row name="学部" vis={vis.faculty} value={p.faculty ?? undefined} onEdit={() => setSpec(field("学部を変更", "学部", p.faculty ?? "", "faculty", { maxLength: 64 }))} />
              <Row name="学科" vis={vis.department} value={p.department ?? undefined} onEdit={() => setSpec(field("学科を変更", "学科", p.department ?? "", "department", { maxLength: 64 }))} />
              <Row name="学年" vis={vis.schoolYear} value={p.schoolYear ? `${p.schoolYear}年` : undefined} onEdit={() => setSpec({ title: "学年を変更", label: "学年（1〜10）", kind: "number", value: p.schoolYear ? String(p.schoolYear) : "", visibility: vis.schoolYear, onSave: async (v, nv) => { await patchMyProfile({ schoolYear: v === "" ? null : Number(v) }); if (nv && nv !== vis.schoolYear) await putVisibility({ ...vis, schoolYear: nv }); await reload(); toast("success", "学年を保存しました。"); } })} />
            </ul>
          </section>

          <section className="panel" aria-labelledby="s-links"><h2 id="s-links">外部リンク（最大10件）</h2>
            <p className="hint">GitHubやポートフォリオなど。https:// か http:// で始まるURLだけ登録できます。</p>
            <ul className="settings">
              {p.links.map((l) => (
                <Row key={l.id} name={l.label} vis={l.visibility} value={l.url}
                  onEdit={() => setSpec({ title: `「${l.label}」のURLを変更`, label: "URL", kind: "url", value: l.url, visibility: l.visibility, onSave: async (v, nv) => { await patchLink(l.id, { url: v, ...(nv ? { visibility: nv } : {}) }); await reload(); toast("success", "リンクを保存しました。"); } })}>
                  <Button variant="danger" fullWidth={false} disabled={busy} aria-label={`${l.label}を削除`} onClick={() => act(() => deleteLink(l.id), `「${l.label}」を削除しました。`)}>削除</Button>
                </Row>
              ))}
              {p.links.length === 0 && <li className="hint">リンクはまだありません。</li>}
            </ul>
            <AddLink onAdd={(label, url, v) => act(() => addLink({ label, url, visibility: v }), "リンクを追加しました。")} busy={busy} />
          </section>

          <section className="panel" aria-labelledby="s-devices"><h2 id="s-devices">使用機材・制作環境（最大20件）</h2>
            <p className="hint">自分のPCやVR機器など。団体が貸し出す物品とは別です。</p>
            <ul className="settings">
              {p.devices.map((d) => (
                <Row key={d.id} name={d.name} vis={d.visibility} value={[d.category, d.spec].filter(Boolean).join(" ・ ")}
                  onEdit={() => setSpec({ title: `「${d.name}」の詳細を変更`, label: "詳細", kind: "textarea", value: d.spec, maxLength: 500, visibility: d.visibility, onSave: async (v, nv) => { await patchDevice(d.id, { spec: v, ...(nv ? { visibility: nv } : {}) }); await reload(); toast("success", "機材を保存しました。"); } })}>
                  <Button variant="danger" fullWidth={false} disabled={busy} aria-label={`${d.name}を削除`} onClick={() => act(() => deleteDevice(d.id), `「${d.name}」を削除しました。`)}>削除</Button>
                </Row>
              ))}
              {p.devices.length === 0 && <li className="hint">機材はまだありません。</li>}
            </ul>
            <AddDevice busy={busy} onAdd={(c, n, s, v) => act(() => addDevice({ category: c, name: n, spec: s, visibility: v }), "機材を追加しました。")} />
          </section>
        </>
      )}

      {tab === "visibility" && (
        <>
          <section className="panel" aria-labelledby="s-vis"><h2 id="s-vis">どこに表示されるか</h2>
            <table className="vis-table">
              <thead><tr><th scope="col">項目</th><th scope="col">本人・管理者</th><th scope="col">メンバー</th><th scope="col">公式サイト</th></tr></thead>
              <tbody>
                <VisRow name="表示名・アイコン" m="常に表示" s="制作クレジットの名前（下の同意があるときだけ）" />
                <VisRow name="自己紹介" m={vis.bio === "members" ? "表示" : "表示しない"} s="表示しない" />
                <VisRow name="学部・学科・学年" m={[vis.faculty, vis.department, vis.schoolYear].map((v) => (v === "members" ? "表示" : "非公開")).join(" / ")} s="表示しない" />
                <VisRow name="外部リンク・使用機材" m="項目ごとに設定" s="表示しない" />
                <VisRow name="氏名・大学メール・学生証番号・最寄り駅" m="表示しない（公開設定もできません）" s="表示しない" />
              </tbody>
            </table>
            <p className="hint">各項目の公開範囲は、「プロフィール」の項目の「変更」から変えられます。</p>
          </section>
          <section className="panel" aria-labelledby="consent-title"><h2 id="consent-title">公式サイトへの公開（制作クレジット）</h2>
            <p>
              作品などの制作メンバーとして、あなたの名前を<strong>インターネット上の公式サイトに載せてよいか</strong>を決めます。メンバーへの公開とは別の設定で、いつでも取り消せます。取り消すと、公式サイトでの表示は「匿名のメンバー」に置き換わります（役割だけが残ります）。同意していない間も、同じく「匿名のメンバー」と表示されます。
            </p>
            <p>現在: <strong>{p.publicCreditConsent.granted ? "同意しています" : "同意していません"}</strong></p>
            <Button fullWidth={false} disabled={busy} onClick={() => act(() => putConsent(!p.publicCreditConsent.granted), p.publicCreditConsent.granted ? "同意を取り消しました。" : "同意しました。")}>
              {p.publicCreditConsent.granted ? "同意を取り消す" : "インターネット公開に同意する"}
            </Button>
          </section>
        </>
      )}

      {tab === "private" && (
        <section className="panel" aria-labelledby="s-private"><h2 id="s-private">個人情報（任意）</h2>
          <p className="notice-inline"><strong>この4項目は、あなた本人と、権限のある管理者（Admin・Owner）だけが見られます。</strong>ほかのメンバーには決して表示されず、公開設定もできません。入力は任意です。</p>
          <ul className="settings">
            <Row name="氏名" vis="fixed-private" value={p.private.legalName ?? undefined} onEdit={() => setSpec(privateField("氏名を変更", "氏名", "legalName", { maxLength: 100 }))} />
            <Row name="大学メール" vis="fixed-private" value={p.private.universityEmail ?? undefined} onEdit={() => setSpec(privateField("大学メールを変更", "大学メール", "universityEmail", { kind: "email", maxLength: 254 }))} />
            <Row name="学生証番号" vis="fixed-private" value={p.private.studentNumber ?? undefined} onEdit={() => setSpec(privateField("学生証番号を変更", "学生証番号", "studentNumber", { maxLength: 32, hint: "半角英数字とハイフンで入力します。" }))} />
            <Row name="最寄り駅" vis="fixed-private" value={p.private.nearestStation ?? undefined} onEdit={() => setSpec(privateField("最寄り駅を変更", "最寄り駅", "nearestStation", { maxLength: 50 }))} />
          </ul>
        </section>
      )}

      {tab === "account" && (
        <>
          <section className="panel" aria-labelledby="s-acc"><h2 id="s-acc">ログイン</h2>
            <ul className="settings">
              <Row name="ぬでじID" value={loginId}><span className="hint s-note">ログインに使うIDです。変更は管理者に依頼してください。</span></Row>
              <Row name="パスワード" value="●●●●●●●●"><span className="hint s-note">変更したいとき・忘れたときは、管理者に「再設定用のURL」の発行を頼んでください。</span></Row>
            </ul>
          </section>
          <section className="panel" aria-labelledby="works-title"><h2 id="works-title">制作実績</h2>
            {p.works.length === 0 ? (
              <p className="hint">公開された作品で、制作メンバーとして登録されたものがここに出ます。まだありません。</p>
            ) : (
              <ul className="plain" data-testid="works">
                {p.works.map((w) => (<li key={`${w.section}/${w.slug}`}><strong>{w.title}</strong> <span className="user-id">{w.roles.join("・")}</span></li>))}
              </ul>
            )}
            <p className="hint">他のメンバーには、「公開設定」で公式サイトへの公開に同意している間だけ、この一覧が見えます。</p>
          </section>
        </>
      )}

      <FieldEditor spec={spec} onClose={() => setSpec(null)} />
      <AvatarEditor open={avatarOpen} onClose={() => setAvatarOpen(false)} name={p.displayName} currentId={p.avatarMediaId} onDone={(t) => { setAvatarOpen(false); void reload(); toast("success", t); }} />
    </main>
  );
}

function VisRow({ name, m, s }: { name: string; m: string; s: string }) {
  return <tr><th scope="row">{name}</th><td>表示</td><td>{m}</td><td>{s}</td></tr>;
}

/** リンクの追加: 名前・URL・公開範囲を1つのダイアログで入力する。 */
function AddLink({ onAdd, busy }: { onAdd: (label: string, url: string, v: Visibility) => void; busy: boolean }) {
  const [open, setOpen] = useState(false);
  const [label, setLabel] = useState("");
  const [url, setUrl] = useState("");
  const [v, setV] = useState<Visibility>("private");
  return (
    <>
      <Button variant="default" fullWidth={false} disabled={busy} onClick={() => setOpen(true)}>リンクを追加する</Button>
      {open && (
        <Dialog open title="リンクを追加" onClose={() => setOpen(false)} actions={<Button fullWidth={false} type="submit" form="link-form" disabled={!label.trim() || !url.trim()}>追加する</Button>}>
          <form id="link-form" onSubmit={(e) => { e.preventDefault(); onAdd(label.trim(), url.trim(), v); setLabel(""); setUrl(""); setV("private"); setOpen(false); }}>
            <div className="field"><TextField id="link-label" label="リンクの名前（例: GitHub）" value={label} onChange={(x: string) => setLabel(x)} maxLength={50} /></div>
            <div className="field"><TextField id="link-url" label="URL" inputMode="url" value={url} onChange={(x: string) => setUrl(x)} autoCapitalize="none" spellCheck={false} /></div>
            <Select id="link-vis" label="公開範囲" value={v} onChange={(x) => setV(x as Visibility)}>
              <option value="private">{VIS_LABEL.private}</option><option value="members">{VIS_LABEL.members}</option>
            </Select>
          </form>
        </Dialog>
      )}
    </>
  );
}

/** 機材の追加: 種類・名前を順に入力する（詳細・公開範囲は追加後に「変更」で設定できる）。 */
function AddDevice({ onAdd, busy }: { onAdd: (c: string, n: string, s: string, v: Visibility) => void; busy: boolean }) {
  const [open, setOpen] = useState(false);
  const [c, setC] = useState("");
  const [n, setN] = useState("");
  const [s, setS] = useState("");
  const [v, setV] = useState<Visibility>("private");
  return (
    <>
      <Button variant="default" fullWidth={false} disabled={busy} onClick={() => setOpen(true)}>機材を追加する</Button>
      {open && <DeviceDialog {...{ c, setC, n, setN, s, setS, v, setV }} onClose={() => setOpen(false)} onSubmit={() => { onAdd(c.trim(), n.trim(), s.trim(), v); setC(""); setN(""); setS(""); setV("private"); setOpen(false); }} />}
    </>
  );
}

function DeviceDialog({ c, setC, n, setN, s, setS, v, setV, onClose, onSubmit }: { c: string; setC: (x: string) => void; n: string; setN: (x: string) => void; s: string; setS: (x: string) => void; v: Visibility; setV: (x: Visibility) => void; onClose: () => void; onSubmit: () => void }) {
  return (
    <Dialog open title="機材を追加" onClose={onClose} actions={<Button fullWidth={false} type="submit" form="device-form" disabled={!c.trim() || !n.trim()}>追加する</Button>}>
      <form id="device-form" onSubmit={(e) => { e.preventDefault(); onSubmit(); }}>
        <div className="field"><TextField id="dev-cat" label="種類（例: PC、VR機器）" value={c} onChange={(x: string) => setC(x)} maxLength={30} /></div>
        <div className="field"><TextField id="dev-name" label="名前" value={n} onChange={(x: string) => setN(x)} maxLength={100} /></div>
        <Textarea id="dev-spec" label="詳細（任意）" rows={2} maxLength={500} value={s} onChange={setS} />
        <Select id="dev-vis" label="公開範囲" value={v} onChange={(x) => setV(x as Visibility)}>
          <option value="private">{VIS_LABEL.private}</option><option value="members">{VIS_LABEL.members}</option>
        </Select>
      </form>
    </Dialog>
  );
}
