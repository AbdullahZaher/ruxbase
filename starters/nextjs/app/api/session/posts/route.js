import { isSafeMutation, json, parseJson, requireSession, setSession, unauthorized } from "../../../../src/server/session.mjs";
export const runtime = "nodejs";
export const dynamic = "force-dynamic";
export async function GET(request) {
  const session = await requireSession(request);
  if (!session) return unauthorized();
  try {
    const page = await session.pb.collection("posts").getList(1, 50, { sort: "-created", requestKey: null });
    return setSession(json({ items: page.items.map(x => ({
      id: x.id, title: x.title, published: x.published, author: x.author,
    })) }), session.token);
  } catch { return json({ error: "Unable to read posts" }, 502); }
}
export async function POST(request) {
  if (!isSafeMutation(request)) return json({ error: "Origin or content type rejected" }, 403);
  const session = await requireSession(request);
  if (!session) return unauthorized();
  const body = await parseJson(request);
  if (!body || typeof body.title !== "string" || body.title.trim().length < 2 || body.title.length > 180) {
    return json({ error: "A title between 2 and 180 characters is required" }, 400);
  }
  try {
    // Client-provided author, published, and other fields are deliberately ignored.
    const post = await session.pb.collection("posts").create({
      title: body.title.trim(), author: session.user.id, published: false,
    }, { requestKey: null });
    return setSession(json({ id: post.id, title: post.title, author: post.author, published: post.published }, 201), session.token);
  } catch { return json({ error: "Unable to create draft" }, 502); }
}
