// 開発用モック（apps/admin/mock）と実APIの応答の「形」（キーの集合と、値が配列・オブジェクトかどうか）を比べる。
// compose smoke（CI）から呼ぶ。値そのものは比べない（データは環境ごとに違うため）。
// 実APIの応答が空の配列などで中身を比べられない場合は、比べられた範囲だけを見る。

/** 比べるGETのAPI（ownerでログインした状態）。:item・:content・:member は実APIの一覧の先頭のIDで置き換える */
export const CONTRACT_PATHS = [
  '/auth/me', '/admin/users?limit=50', '/members?limit=50', '/members/:member', '/me/profile',
  '/inventory/items?limit=50', '/inventory/items/:item', '/inventory/loans?limit=50', '/inventory/history?limit=50',
  '/contents?limit=50', '/contents/:content', '/admin/notifications/settings', '/admin/notifications/deliveries?limit=50',
  '/admin/legacy-authors',
];

const kind = (v) => (v === null || v === undefined ? 'null' : Array.isArray(v) ? 'array' : typeof v === 'object' ? 'object' : 'scalar');

/** 中身のキーが記事ごとに自由な入れ物（Record<string, string>）。オブジェクトであることだけを比べ、中のキーは比べない */
export const FREEFORM_KEYS = ['metadata'];

/** 形の違いを文字列の配列で返す（空なら一致）。optional に挙げたキーは片方に無くてもよい。 */
export function compareShapes(mock, real, optional = [], path = '$') {
  if (FREEFORM_KEYS.some((k) => path.endsWith(`.${k}`))) return kind(mock) === kind(real) || kind(mock) === 'null' || kind(real) === 'null' ? [] : [`${path}: モック=${kind(mock)} 実API=${kind(real)}`];
  const diffs = [];
  const km = kind(mock), kr = kind(real);
  if (km === 'null' || kr === 'null') return diffs; // nullを許す項目（未設定など）は形を比べられない
  if (km !== kr) return [`${path}: モック=${km} 実API=${kr}`];
  if (km === 'array') {
    if (mock.length && real.length) diffs.push(...compareShapes(mock[0], real[0], optional, `${path}[0]`));
    return diffs;
  }
  if (km !== 'object') return diffs;
  const opt = new Set(optional);
  for (const k of Object.keys(real)) if (!(k in mock) && !opt.has(k)) diffs.push(`${path}.${k}: 実APIにあり、モックに無い`);
  for (const k of Object.keys(mock)) if (!(k in real) && !opt.has(k)) diffs.push(`${path}.${k}: モックにあり、実APIに無い`);
  for (const k of Object.keys(mock)) if (k in real) diffs.push(...compareShapes(mock[k], real[k], optional, `${path}.${k}`));
  return diffs;
}
