// liveness用。外部依存を確認しない。
export const dynamic = "force-static";

export function GET() {
  return Response.json({ status: "ok" }, { headers: { "Cache-Control": "no-store" } });
}
