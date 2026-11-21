// liveness用。公開APIなど外部に依存しない（APIが落ちていても、コンテナを再起動させない）。
export function GET() {
  return Response.json({ status: "ok" }, { headers: { "Cache-Control": "no-store" } });
}
