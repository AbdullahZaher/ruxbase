import { isSafeMutation, json, newClient, parseJson, setSession } from "../../../../src/server/session.mjs";
export const runtime = "nodejs";
export const dynamic = "force-dynamic";
export async function POST(request) {
  if (!isSafeMutation(request)) return json({ error: "Origin or content type rejected" }, 403);
  const body = await parseJson(request);
  if (!body || typeof body.email !== "string" || body.email.length > 254 ||
      typeof body.password !== "string" || !body.password || body.password.length > 4096) {
    return json({ error: "Valid email and password are required" }, 400);
  }
  try {
    const pb = newClient();
    const auth = await pb.collection("users").authWithPassword(body.email, body.password, { requestKey: null });
    if (!auth?.record?.id || !auth?.token) return json({ error: "Authentication rejected" }, 401);
    return setSession(json({ user: { id: auth.record.id, email: auth.record.email } }), auth.token);
  } catch { return json({ error: "Authentication rejected" }, 401); }
}
