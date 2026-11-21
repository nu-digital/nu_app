import TokenPasswordForm from "@/components/TokenPasswordForm";

export const metadata = { title: "パスワードの再設定 | nu_app", referrer: "no-referrer" as const };

export default function ResetPage() {
  return (
    <TokenPasswordForm
      kind="reset-password"
      title="パスワードを再設定する"
      lead="新しいパスワードを設定してください。設定すると、ほかの端末のログインはすべて解除されます。"
      done="パスワードを再設定しました。新しいパスワードでログインしてください。"
    />
  );
}
