// 開発用モックの架空データ。人名・ID・メールなどはすべて架空（実在の人・団体と無関係）。
// 型は画面と同じ lib/api.ts の型で宣言する。型がずれると admin の tsc が失敗する。
// 実APIの応答との形の比較は scripts/compose-smoke.mjs（CI）で行う。
// このディレクトリはアプリ本体からimportしない（lib/no-mock-import.test.mjs で検査）。Dockerイメージにも入れない（.dockerignore）。
import type {
  AdminUser, ContentDetail, ContentListItem, Delivery, HistoryEvent, Item, ItemDetail, LegacyAuthor, Member, MyProfile,
  NotificationSetting, OutstandingLoan, PublishedHit,
} from "../lib/api";

export type Role = "owner" | "admin" | "editor" | "member";

const T = "2026-11-28T10:00:00+09:00";

/** ロールごとのログインID。パスワードは何でもよい（モックは検査しない）。 */
export const ROLE_USERS: Record<Role, { id: string; loginId: string; displayName: string; roles: string[] }> = {
  owner: { id: "u-owner", loginId: "owner", displayName: "見本 太郎", roles: ["member", "editor", "admin", "owner"] },
  admin: { id: "u-admin", loginId: "admin", displayName: "試験 花子", roles: ["member", "editor", "admin"] },
  editor: { id: "u-editor", loginId: "editor", displayName: "仮名 次郎", roles: ["member", "editor"] },
  member: { id: "u-member", loginId: "member", displayName: "例示 三郎", roles: ["member"] },
};

export function seed() {
  const users: AdminUser[] = [
    ...Object.values(ROLE_USERS).map((u) => ({ ...u, status: "active" as const, createdAt: T, activatedAt: T })),
    { id: "u-invited", loginId: "invited", displayName: "招待 四郎", status: "invited" as const, roles: ["member"], createdAt: T, activatedAt: null },
    { id: "u-disabled", loginId: "disabled", displayName: "停止 五子", status: "disabled" as const, roles: ["member"], createdAt: T, activatedAt: T },
  ];

  const members: Member[] = [
    { id: "u-owner", avatarMediaId: "m-avatar", displayName: "見本 太郎", bio: "3DCGと映像を作っています。", faculty: "芸術学部", department: "デザイン学科", schoolYear: 3,
      links: [{ id: "l-1", label: "ポートフォリオ", url: "https://example.com/" }], devices: [{ id: "d-1", category: "PC", name: "自作デスクトップ", spec: "メモリ32GB" }],
      works: [{ section: "project", slug: "sample-work", title: "見本の作品", roles: ["モデリング"] }] },
    { id: "u-admin", avatarMediaId: null, displayName: "試験 花子", bio: null, faculty: "芸術学部", department: "映画学科", schoolYear: 2, links: [], devices: [], works: [] },
    { id: "u-editor", avatarMediaId: null, displayName: "仮名 次郎", bio: "イラストを描きます。とても長い自己紹介の例です。".repeat(3), faculty: null, department: null, schoolYear: null, links: [], devices: [], works: [] },
    { id: "u-member", avatarMediaId: null, displayName: "例示 三郎", bio: null, faculty: "理工学部", department: null, schoolYear: 1, links: [], devices: [], works: [] },
  ];

  const profile: MyProfile = {
    avatarMediaId: "m-avatar", displayName: "見本 太郎", bio: "3DCGと映像を作っています。", faculty: "芸術学部", department: "デザイン学科", schoolYear: 3,
    visibility: { bio: "members", faculty: "members", department: "private", schoolYear: "members" },
    private: { legalName: "見本 太郎", studentNumber: "X0000000", universityEmail: "sample@example.invalid", nearestStation: "見本駅" },
    links: [{ id: "l-1", label: "ポートフォリオ", url: "https://example.com/", sortOrder: 0, visibility: "members" }],
    devices: [{ id: "d-1", category: "PC", name: "自作デスクトップ", spec: "メモリ32GB", sortOrder: 0, visibility: "members" }],
    publicCreditConsent: { granted: true, grantedAt: T, withdrawnAt: null },
    works: [{ section: "project", slug: "sample-work", title: "見本の作品", roles: ["モデリング"] }],
  };

  const items: Item[] = [
    { id: "i-camera", name: "ミラーレスカメラ", description: "撮影用。レンズ2本つき。", category: "カメラ", location: "部室 棚A", mode: "individual", totalQuantity: 2, outstanding: 1, unitCount: 2, available: 1, archived: false, photoMediaId: "m-camera" },
    { id: "i-tablet", name: "ペンタブレット", description: "", category: "PC周辺", location: "部室 棚B", mode: "quantity", totalQuantity: 5, outstanding: 5, unitCount: 0, available: 0, archived: false, photoMediaId: null },
    { id: "i-tripod", name: "三脚", description: "", category: "カメラ", location: "部室 ロッカー", mode: "quantity", totalQuantity: 4, outstanding: 1, unitCount: 0, available: 3, archived: false, photoMediaId: null },
    { id: "i-vr", name: "VRヘッドセット", description: "動作確認用。", category: "VR", location: "部室 棚C", mode: "individual", totalQuantity: 1, outstanding: 0, unitCount: 1, available: 1, archived: false, photoMediaId: "m-vr" },
  ];

  const units: Record<string, ItemDetail["units"]> = {
    "i-camera": [
      { id: "un-1", assetTag: "CAM-001", serialNumber: "SN-0001", state: "active", loaned: { borrowerDisplayName: "試験 花子", since: T, elapsedDays: 3 } },
      { id: "un-2", assetTag: "CAM-002", serialNumber: null, state: "active", loaned: null },
    ],
    "i-vr": [{ id: "un-3", assetTag: "VR-001", serialNumber: null, state: "active", loaned: null }],
  };

  const loans: OutstandingLoan[] = [
    { id: "lo-1", itemId: "i-camera", itemName: "ミラーレスカメラ", assetTag: "CAM-001", borrowerDisplayName: "試験 花子", quantity: 1, borrowedAt: T, elapsedDays: 3, mine: false },
    { id: "lo-2", itemId: "i-tablet", itemName: "ペンタブレット", assetTag: "", borrowerDisplayName: "見本 太郎", quantity: 5, borrowedAt: T, elapsedDays: 12, mine: true },
    { id: "lo-3", itemId: "i-tripod", itemName: "三脚", assetTag: "", borrowerDisplayName: "例示 三郎", quantity: 1, borrowedAt: T, elapsedDays: 1, mine: false },
  ];

  const history: HistoryEvent[] = [
    { id: "h-1", kind: "loan", itemId: "i-camera", itemName: "ミラーレスカメラ", assetTag: "CAM-001", borrowerDisplayName: "試験 花子", actorDisplayName: "試験 花子", quantity: 1, at: T },
    { id: "h-2", kind: "return", itemId: "i-tripod", itemName: "三脚", assetTag: "", borrowerDisplayName: "見本 太郎", actorDisplayName: "見本 太郎", quantity: 1, at: T },
    { id: "h-3", kind: "correction", itemId: "i-tablet", itemName: "ペンタブレット", assetTag: "", borrowerDisplayName: "例示 三郎", actorDisplayName: "見本 太郎", quantity: 1, at: T, correctionKind: "void", targetType: "loan", reason: "記録の誤り" },
  ];

  const contents: ContentListItem[] = [
    { id: "c-1", type: "article", section: "blog", slug: "sample-blog", title: "学園祭の展示レポート", summary: "展示の様子をまとめました。", revisionNo: 3, isPublished: true, canEdit: true, hasUnpublishedChanges: true, updatedAt: T },
    { id: "c-2", type: "project", section: "project", slug: "sample-work", title: "見本の作品", summary: "短編アニメーション。", revisionNo: 1, isPublished: false, canEdit: true, hasUnpublishedChanges: true, updatedAt: T },
    { id: "c-3", type: "article", section: "news", slug: "sample-news", title: "新歓のお知らせ", summary: "", revisionNo: 2, isPublished: true, canEdit: false, hasUnpublishedChanges: false, updatedAt: T },
  ];

  const contentDetail = (c: ContentListItem): ContentDetail => ({
    id: c.id, type: c.type, section: c.section, slug: c.slug, creator: { userId: "u-owner", displayName: "見本 太郎" },
    workingRevision: {
      revisionNo: c.revisionNo, title: c.title, summary: c.summary,
      markdown: "## 展示の様子\n\n今年は**3DCG**と映像を展示しました。\n\n- 来場者アンケート\n- 次回の改善点\n",
      metadata: { category: "活動報告" }, date: { label: "2026年11月3日", kind: "day", sortDate: "2026-11-03", sortDateIsEstimated: false },
      editedBy: "見本 太郎", createdAt: T, coverMedia: null, contributors: [{ userId: "u-admin", displayName: "試験 花子", roleLabel: "撮影" }],
    },
    publishedRevisionNo: c.isPublished ? Math.max(1, c.revisionNo - 1) : null,
    collaborators: [{ userId: "u-admin", displayName: "試験 花子" }],
    revisions: [{ revisionNo: c.revisionNo, title: c.title, editedBy: "見本 太郎", createdAt: T }],
    canManageCollaborators: c.canEdit, canPublish: c.canEdit, createdAt: T, updatedAt: T,
  });

  const notificationSettings: NotificationSetting[] = [
    { eventType: "inventory.loan_created", label: "物品の貸出", enabled: true, discordChannelId: "100000000000000001", updatedAt: T },
    { eventType: "inventory.loan_returned", label: "物品の返却", enabled: true, discordChannelId: "100000000000000001", updatedAt: T },
    { eventType: "inventory.correction_created", label: "貸出記録の訂正", enabled: false, discordChannelId: null, updatedAt: T },
    { eventType: "content.published", label: "記事の公開", enabled: false, discordChannelId: null, updatedAt: T },
  ];

  const deliveries: Delivery[] = [
    { id: "dl-1", eventType: "inventory.loan_created", label: "物品の貸出", status: "dead", attempts: 5, nextAttemptAt: T, lastError: "Discordに接続できません（架空）", createdAt: T, sentAt: null },
    { id: "dl-2", eventType: "inventory.loan_created", label: "物品の貸出", status: "sent", attempts: 1, nextAttemptAt: T, lastError: null, createdAt: T, sentAt: T },
  ];

  const legacyAuthors: LegacyAuthor[] = [
    { id: "la-1", name: "旧メンバーA（架空）", publicationState: "unconfirmed", confirmedAt: null, withdrawnAt: null, erased: false, linkedUserId: null, linkedDisplayName: null, contentCount: 4 },
    { id: "la-2", name: "旧メンバーB（架空）", publicationState: "permitted", confirmedAt: T, withdrawnAt: null, erased: false, linkedUserId: "u-admin", linkedDisplayName: "試験 花子", contentCount: 1 },
  ];

  const publishedHits: PublishedHit[] = [{ section: "blog", slug: "sample-blog", title: "学園祭の展示レポート", found: ["markdown"], snippet: "…撮影: 試験 花子…" }];

  return { users, members, profile, items, units, loans, history, contents, contentDetail, notificationSettings, deliveries, legacyAuthors, publishedHits, now: T };
}

/**
 * 実APIにあってもデータ次第で省略されうるキー（lib/api.ts で `?` のもの）。
 * 契約比較では「モックにあって実APIに無い」場合だけ、ここに挙げたキーを許す。
 */
export const OPTIONAL_KEYS = ["archived", "links", "devices", "works", "contributors", "revisions", "correctionKind", "targetType", "reason", "user", "snippet"];
