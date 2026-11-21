import { mediaUrl } from "@/lib/api";

/** アバター・物品の写真（ログイン中のメンバーだけが取得できる。最適化・キャッシュをしない）。 */
export default function MediaImg({ id, className, alt = "", lazy }: { id: string; className?: string; alt?: string; lazy?: boolean }) {
  // eslint-disable-next-line @next/next/no-img-element
  return <img className={className} src={mediaUrl(id)} alt={alt} loading={lazy ? "lazy" : undefined} />;
}
