import { json, requireSession, tokenFrom, unauthorized, setSession } from "../../../../src/server/session.mjs";
export const runtime = "nodejs";
export const dynamic = "force-dynamic";
export async function GET(request) {
  const session = await requireSession(request);
  if (!session) return unauthorized();
  return setSession(json({ user: { id: session.user.id, email: session.user.email } }), session.token);
}
