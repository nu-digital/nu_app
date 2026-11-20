// 団体のロゴ（既存サイトの団体素材。D13で継続公開が許可されたもの）。
export default function Brand() {
  return (
    <div className="auth-brand">
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/images/logo.webp" alt="" width={48} height={48} />
      <span>nu_digital ポータル</span>
    </div>
  );
}
