import { clearSession, isSafeMutation, json } from "../../../../src/server/session.mjs";
export const runtime = "nodejs";
export const dynamic = "force-dynamic";
export async function POST(request) {
  if (!isSafeMutation(request)) return json({ error: "Origin or content type rejected" }, 403);
  return clearSession(json({ ok: true }));
}
