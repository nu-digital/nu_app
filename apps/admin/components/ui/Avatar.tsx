import MediaImg from "@/components/MediaImg";

/** メンバーのアイコン。画像が無いときは表示名の頭文字を丸に表示する。画像はログイン中のメンバーだけが取得できる。 */
export function Avatar({ name, mediaId, size = 32 }: { name: string; mediaId?: string | null; size?: number }) {
  return (
    <span className="ui-avatar" style={{ width: size, height: size, fontSize: Math.round(size * 0.45) }} title={name}>
      {mediaId ? <MediaImg id={mediaId} alt="" lazy /> : <span aria-hidden="true">{Array.from(name.trim())[0] ?? "?"}</span>}
    </span>
  );
}

export type Person = { userId: string; displayName: string; avatarMediaId?: string | null };

/**
 * 複数のメンバーを重ねて表示する（GitHubの共同編集者のように）。名前はtitle・読み上げ・（クリックで）一覧に出る。
 * max人まで表示し、残りは「+n」。
 */
export function AvatarStack({ people, max = 4, size = 28, label = "関わっているメンバー" }: { people: Person[]; max?: number; size?: number; label?: string }) {
  if (people.length === 0) return null;
  const shown = people.slice(0, max);
  const rest = people.length - shown.length;
  const names = people.map((p) => p.displayName).join("、");
  return (
    <details className="ui-avatar-stack">
      <summary aria-label={`${label}: ${names}`} title={names}>
        <span className="stack" aria-hidden="true">
          {shown.map((p) => <Avatar key={p.userId} name={p.displayName} mediaId={p.avatarMediaId} size={size} />)}
          {rest > 0 && <span className="ui-avatar more" style={{ width: size, height: size, fontSize: Math.round(size * 0.4) }}>+{rest}</span>}
        </span>
      </summary>
      <ul className="people-pop">
        {people.map((p) => <li key={p.userId}><Avatar name={p.displayName} mediaId={p.avatarMediaId} size={24} /> {p.displayName}</li>)}
      </ul>
    </details>
  );
}
