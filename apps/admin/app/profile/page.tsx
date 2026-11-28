"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";

import AppShell from "@/components/AppShell";
import ImageUploader from "@/components/ImageUploader";
import Notice from "@/components/Notice";
import Button from "@/components/ui/Button";
import TextField from "@/components/ui/TextField";
import {
  addDevice, addLink, clearAvatar, setAvatar, deleteDevice, deleteLink, describe, getMyProfile, patchDevice, patchLink, patchMyProfile, putConsent, putVisibility,
  type MyProfile, type Visibility,
} from "@/lib/api";

const VIS_LABEL: Record<Visibility, string> = { private: "非公開（自分と管理者だけ）", members: "メンバーに公開" };

export default function ProfilePage() {
  return <AppShell>{() => <Profile />}</AppShell>;
}

function VisibilitySelect({ id, value, onChange, label }: { id: string; value: Visibility; onChange: (v: Visibility) => void; label: string }) {
  return (
    <div className="vis">
      <label htmlFor={id}>{label}の公開範囲</label>
      <select id={id} className="input" value={value} onChange={(e) => onChange(e.target.value as Visibility)}>
        <option value="private">{VIS_LABEL.private}</option>
        <option value="members">{VIS_LABEL.members}</option>
      </select>
    </div>
  );
}

function Profile() {
  const [p, setP] = useState<MyProfile | null>(null);
  const [msg, setMsg] = useState<{ kind: "error" | "success"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    try {
      setP(await getMyProfile());
    } catch (e) {
      setMsg({ kind: "error", text: describe(e) });
    }
  }, []);
  useEffect(() => {
    let alive = true;
    getMyProfile()
      .then((v) => alive && setP(v))
      .catch((e) => alive && setMsg({ kind: "error", text: describe(e) }));
    return () => {
      alive = false;
    };
  }, []);

  const run = async (f: () => Promise<unknown>, done: string) => {
    if (busy) return;
    setBusy(true);
    setMsg(null);
    try {
      await f();
      await reload();
      setMsg({ kind: "success", text: done });
    } catch (e) {
      setMsg({ kind: "error", text: describe(e) });
    } finally {
      setBusy(false);
    }
  };

  if (!p) return <main className="page" aria-busy="true">{msg && <Notice kind={msg.kind}>{msg.text}</Notice>}</main>;

  return (
    <main className="page">
      <h1>プロフィール</h1>
      <p className="lead">表示名だけが必須です。そのほかの項目は入力しなくても、すべての機能を使えます。初期設定ではすべて非公開です。</p>
      {msg && <Notice kind={msg.kind}>{msg.text}</Notice>}

      <section className="panel" aria-labelledby="avatar-title">
        <h2 id="avatar-title">アバター</h2>
        <p className="hint">メンバー一覧・プロフィールで、ログイン中のメンバーだけに表示されます（インターネットには公開されません）。顔が写った写真を使う場合は、公開してよいか確認してください。</p>
        <ImageUploader
          label="アバター" purpose="avatar" large currentId={p.avatarMediaId} busy={busy}
          attach={async (id) => { await setAvatar(id); await reload(); }}
          detach={async () => { await clearAvatar(); await reload(); }}
          onError={(text) => setMsg({ kind: "error", text })} onDone={(text) => setMsg({ kind: "success", text })}
        />
      </section>

      <BasicSection key={JSON.stringify([p.displayName, p.bio, p.faculty, p.department, p.schoolYear, p.visibility])} p={p} busy={busy} run={run} />
      <PrivateSection key={JSON.stringify(p.private)} p={p} busy={busy} run={run} />
      <LinksSection p={p} busy={busy} run={run} />
      <DevicesSection p={p} busy={busy} run={run} />

      <section className="panel" aria-labelledby="works-title">
        <h2 id="works-title">制作実績</h2>
        {p.works.length === 0 ? (
          <p className="hint">公開された作品で、制作メンバーとして登録されたものがここに出ます。まだありません。</p>
        ) : (
          <ul className="plain" data-testid="works">
            {p.works.map((w) => (
              <li key={`${w.section}/${w.slug}`}>
                <strong>{w.title}</strong> <span className="user-id">{w.roles.join("・")}</span>
              </li>
            ))}
          </ul>
        )}
        <p className="hint">他のメンバーには、上の「インターネットへの公開」に同意している間だけ、この一覧が見えます。</p>
      </section>

      <section className="panel" aria-labelledby="consent-title">
        <h2 id="consent-title">インターネットへの公開（公開クレジット）</h2>
        <p>
          作品などの制作メンバーとして、あなたの名前を<strong>インターネット上の公式サイトに載せてよいか</strong>を決めます。上の「メンバーに公開」とは別の設定です。いつでも取り消せます。取り消すと、公式サイトでの表示は「匿名のメンバー」に置き換わります（役割だけが残ります）。同意していない間も、同じく「匿名のメンバー」と表示されます。
        </p>
        <p>
          現在: <strong>{p.publicCreditConsent.granted ? "同意しています" : "同意していません"}</strong>
        </p>
        <Button fullWidth={false} type="submit" disabled={busy} onClick={() => run(() => putConsent(!p.publicCreditConsent.granted), p.publicCreditConsent.granted ? "同意を取り消しました。" : "同意しました。")}>
          {p.publicCreditConsent.granted ? "同意を取り消す" : "インターネット公開に同意する"}
        </Button>
      </section>
    </main>
  );
}

type SectionProps = { p: MyProfile; busy: boolean; run: (f: () => Promise<unknown>, done: string) => Promise<void> };

function BasicSection({ p, busy, run }: SectionProps) {
  const [displayName, setDisplayName] = useState(p.displayName);
  const [bio, setBio] = useState(p.bio ?? "");
  const [faculty, setFaculty] = useState(p.faculty ?? "");
  const [department, setDepartment] = useState(p.department ?? "");
  const [year, setYear] = useState(p.schoolYear ? String(p.schoolYear) : "");
  const [vis, setVis] = useState(p.visibility);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    void run(async () => {
      await patchMyProfile({ displayName, bio, faculty, department, schoolYear: year === "" ? null : Number(year) });
      await putVisibility(vis);
    }, "基本情報を保存しました。");
  };
  return (
    <section className="panel" aria-labelledby="basic-title">
      <h2 id="basic-title">基本情報</h2>
      <form onSubmit={submit}>
        <div className="field">
          <TextField id="displayName" label="表示名（必須）" value={displayName} onChange={setDisplayName} required maxLength={64} />
          <p className="hint">ログインしているメンバーに表示されます（64文字まで）。</p>
        </div>
        <div className="field">
          <label htmlFor="bio">自己紹介</label>
          <textarea id="bio" className="input" rows={4} value={bio} onChange={(e) => setBio(e.target.value)} maxLength={1000} />
          <VisibilitySelect id="vis-bio" label="自己紹介" value={vis.bio} onChange={(v) => setVis({ ...vis, bio: v })} />
        </div>
        <div className="field">
          <TextField id="faculty" label="学部" value={faculty} onChange={setFaculty} maxLength={64} />
          <VisibilitySelect id="vis-faculty" label="学部" value={vis.faculty} onChange={(v) => setVis({ ...vis, faculty: v })} />
        </div>
        <div className="field">
          <TextField id="department" label="学科" value={department} onChange={setDepartment} maxLength={64} />
          <VisibilitySelect id="vis-department" label="学科" value={vis.department} onChange={(v) => setVis({ ...vis, department: v })} />
        </div>
        <div className="field">
          <label htmlFor="year">学年（1〜10）</label>
          <input id="year" className="input" type="number" min={1} max={10} inputMode="numeric" value={year} onChange={(e) => setYear(e.target.value)} />
          <VisibilitySelect id="vis-year" label="学年" value={vis.schoolYear} onChange={(v) => setVis({ ...vis, schoolYear: v })} />
        </div>
        <Button fullWidth={false} type="submit" disabled={busy}>基本情報を保存する</Button>
      </form>
    </section>
  );
}

function PrivateSection({ p, busy, run }: SectionProps) {
  const [legal, setLegal] = useState(p.private.legalName ?? "");
  const [email, setEmail] = useState(p.private.universityEmail ?? "");
  const [num, setNum] = useState(p.private.studentNumber ?? "");
  const [station, setStation] = useState(p.private.nearestStation ?? "");
  return (
    <section className="panel" aria-labelledby="private-title">
      <h2 id="private-title">個人情報（任意）</h2>
      <p className="notice-inline"><strong>この4項目は、あなた本人と、権限のある管理者（Admin・Owner）だけが見られます。</strong>ほかのメンバーには決して表示されず、公開設定もできません。入力は任意です。</p>
      <form onSubmit={(e) => { e.preventDefault(); void run(() => patchMyProfile({ legalName: legal, universityEmail: email, studentNumber: num, nearestStation: station }), "個人情報を保存しました。"); }}>
        <div className="field"><TextField id="legal" label="氏名" value={legal} onChange={setLegal} maxLength={100} autoComplete="name" /></div>
        <div className="field"><TextField id="email" label="大学メール" type="email" value={email} onChange={setEmail} maxLength={254} autoComplete="email" /></div>
        <div className="field"><TextField id="num" label="学生証番号" value={num} onChange={setNum} maxLength={32} inputMode="text" /><p className="hint">半角英数字とハイフンで入力します。</p></div>
        <div className="field"><TextField id="station" label="最寄り駅" value={station} onChange={setStation} maxLength={50} /></div>
        <Button fullWidth={false} type="submit" disabled={busy}>個人情報を保存する</Button>
      </form>
    </section>
  );
}

function LinksSection({ p, busy, run }: SectionProps) {
  const [label, setLabel] = useState("");
  const [url, setUrl] = useState("");
  const [vis, setVis] = useState<Visibility>("private");
  return (
    <section className="panel" aria-labelledby="links-title">
      <h2 id="links-title">外部リンク（最大10件）</h2>
      <p className="hint">GitHubやポートフォリオなど。https:// か http:// で始まるURLだけ登録できます。</p>
      <ul className="items">
        {p.links.map((l) => (
          <li key={l.id} className="item" data-label={l.label}>
            <div><strong>{l.label}</strong><div className="user-id">{l.url}</div></div>
            <div className="actions">
              <select aria-label={`${l.label}の公開範囲`} className="input" value={l.visibility} onChange={(e) => run(() => patchLink(l.id, { visibility: e.target.value }), "公開範囲を変更しました。")}>
                <option value="private">{VIS_LABEL.private}</option><option value="members">{VIS_LABEL.members}</option>
              </select>
              <Button variant="default" fullWidth={false} type="submit" disabled={busy} onClick={() => run(() => deleteLink(l.id), `「${l.label}」を削除しました。`)}>削除</Button>
            </div>
          </li>
        ))}
        {p.links.length === 0 && <li className="hint">リンクはまだありません。</li>}
      </ul>
      <form onSubmit={(e) => { e.preventDefault(); void run(async () => { await addLink({ label, url, visibility: vis }); setLabel(""); setUrl(""); setVis("private"); }, "リンクを追加しました。"); }}>
        <div className="field"><TextField id="link-label" label="リンクの名前" value={label} onChange={setLabel} required maxLength={50} /></div>
        <div className="field"><TextField id="link-url" label="URL" type="text" inputMode="url" value={url} onChange={setUrl} required autoCapitalize="none" spellCheck={false} /></div>
        <VisibilitySelect id="link-vis" label="リンク" value={vis} onChange={setVis} />
        <Button fullWidth={false} type="submit" disabled={busy}>リンクを追加する</Button>
      </form>
    </section>
  );
}

function DevicesSection({ p, busy, run }: SectionProps) {
  const [category, setCategory] = useState("");
  const [name, setName] = useState("");
  const [spec, setSpec] = useState("");
  const [vis, setVis] = useState<Visibility>("private");
  return (
    <section className="panel" aria-labelledby="devices-title">
      <h2 id="devices-title">個人所有の機材（最大20件）</h2>
      <p className="hint">自分のPCやVR機器など。団体が貸し出す物品とは別です。</p>
      <ul className="items">
        {p.devices.map((d) => (
          <li key={d.id} className="item" data-name={d.name}>
            <div><strong>{d.name}</strong>（{d.category}）{d.spec && <div className="user-id">{d.spec}</div>}</div>
            <div className="actions">
              <select aria-label={`${d.name}の公開範囲`} className="input" value={d.visibility} onChange={(e) => run(() => patchDevice(d.id, { visibility: e.target.value }), "公開範囲を変更しました。")}>
                <option value="private">{VIS_LABEL.private}</option><option value="members">{VIS_LABEL.members}</option>
              </select>
              <Button variant="default" fullWidth={false} type="submit" disabled={busy} onClick={() => run(() => deleteDevice(d.id), `「${d.name}」を削除しました。`)}>削除</Button>
            </div>
          </li>
        ))}
        {p.devices.length === 0 && <li className="hint">機材はまだありません。</li>}
      </ul>
      <form onSubmit={(e) => { e.preventDefault(); void run(async () => { await addDevice({ category, name, spec, visibility: vis }); setCategory(""); setName(""); setSpec(""); setVis("private"); }, "機材を追加しました。"); }}>
        <div className="field"><TextField id="dev-cat" label="種類（例: PC、VR機器）" value={category} onChange={setCategory} required maxLength={30} /></div>
        <div className="field"><TextField id="dev-name" label="名前" value={name} onChange={setName} required maxLength={100} /></div>
        <div className="field"><label htmlFor="dev-spec">詳細（任意）</label><textarea id="dev-spec" className="input" rows={2} value={spec} onChange={(e) => setSpec(e.target.value)} maxLength={500} /></div>
        <VisibilitySelect id="dev-vis" label="機材" value={vis} onChange={setVis} />
        <Button fullWidth={false} type="submit" disabled={busy}>機材を追加する</Button>
      </form>
    </section>
  );
}
