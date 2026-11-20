import TokenPasswordForm from "@/components/TokenPasswordForm";

export const metadata = { title: "パスワードの設定 | nu_app", referrer: "no-referrer" as const };

export default function InvitePage() {
  return (
    <TokenPasswordForm
      kind="accept-invite"
      title="パスワードを設定する"
      lead="招待ありがとうございます。ログインに使うパスワードを設定してください。"
      done="パスワードを設定しました。ログイン画面からログインしてください。"
    />
  );
}
