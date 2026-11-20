"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import AppShell from "@/components/AppShell";
import Notice from "@/components/Notice";
import { describe, getMember, type Member } from "@/lib/api";

export default function MemberPage() {
  return <AppShell>{() => <Detail />}</AppShell>;
}

function Detail() {
  const { id } = useParams<{ id: string }>();
  const [m, setM] = useState<Member | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    getMember(id).then(setM).catch((e) => setError((e as { status?: number }).status === 404 ? "このメンバーは見つかりません（退会した、または存在しません）。" : describe(e)));
  }, [id]);
  return (
    <main className="page">
      <p><Link href="/members">← メンバー一覧へ</Link></p>
      {error && <Notice kind="error">{error}</Notice>}
      {m && (
        <>
          <h1>{m.displayName}</h1>
          <dl className="facts">
            {m.faculty && (<><dt>学部</dt><dd>{m.faculty}</dd></>)}
            {m.department && (<><dt>学科</dt><dd>{m.department}</dd></>)}
            {m.schoolYear && (<><dt>学年</dt><dd>{m.schoolYear}年</dd></>)}
          </dl>
          {m.bio && (<section><h2>自己紹介</h2><p className="bio">{m.bio}</p></section>)}
          {m.links && m.links.length > 0 && (
            <section><h2>リンク</h2><ul className="plain">{m.links.map((l) => (<li key={l.id}><a href={l.url} rel="noopener noreferrer nofollow" target="_blank">{l.label}</a></li>))}</ul></section>
          )}
          {m.devices && m.devices.length > 0 && (
            <section><h2>機材</h2><ul className="plain">{m.devices.map((d) => (<li key={d.id}><strong>{d.name}</strong>（{d.category}）{d.spec && <span className="user-id"> {d.spec}</span>}</li>))}</ul></section>
          )}
          {!m.bio && !m.faculty && !m.department && !m.schoolYear && !(m.links?.length) && !(m.devices?.length) && <p className="hint">公開されている情報はまだありません。</p>}
        </>
      )}
    </main>
  );
}
