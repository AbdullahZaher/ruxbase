import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { SESSION_COOKIE, verifiedSession } from "../../src/server/session.mjs";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
export default async function ProtectedPage() {
  const store = await cookies();
  const session = await verifiedSession(store.get(SESSION_COOKIE)?.value || "");
  if (!session) redirect("/");
  let posts = [];
  try {
    const data = await session.pb.collection("posts").getList(1, 50, { sort: "-created", requestKey: null });
    posts = data.items;
  } catch { /* show no records on Ruxbase backend errors */ }
  return <main className="shell">
    <header className="topbar"><h1>Protected posts</h1><a href="/">Back to starter</a></header>
    <p>Signed in as {session.user.email}</p>
    <ul>{posts.map(post => <li key={post.id}>{post.title}</li>)}</ul>
    <p>Per-request authenticated server rendering. No user token is sent to this page.</p>
  </main>;
}
