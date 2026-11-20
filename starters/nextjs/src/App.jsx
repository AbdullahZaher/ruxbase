"use client";
import { useEffect, useMemo, useState } from "react";
import PocketBase, { BaseAuthStore } from "pocketbase";

const errorText = (error) => {
  if (error?.status === 404) return "The posts collection is missing. Follow the example schema and rules guide.";
  if (error?.status === 400) return "The request was rejected. Check credentials, collection rules, and field values.";
  if (error?.status === 0) return "Cannot reach Ruxbase. Start the backend and check the /api proxy.";
  return "Request failed. Check Ruxbase readiness and try again.";
};

export default function App() {
  // Per-mounted-app memory-only auth. No shared SSR user client or stored admin token.
  const pb = useMemo(() => new PocketBase("/", new BaseAuthStore()), []);
  const [online, setOnline] = useState(false);
  const [posts, setPosts] = useState([]);
  const [user, setUser] = useState(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [dark, setDark] = useState(true);

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  }, [dark]);

  async function refresh(client = pb) {
    const result = await client.collection("posts").getList(1, 20, { sort: "-created", requestKey: null });
    setPosts(result.items);
  }

  useEffect(() => {
    let active = true;
    pb.health.check().then(() => {
      if (!active) return;
      setOnline(true);
      refresh().catch(e => { if (active) setError(errorText(e)); });
    }).catch(() => { if (active) setError("Cannot reach Ruxbase. Start Docker Compose and try again."); });
    return () => { active = false; };
  }, [pb]);

  async function signIn(event) {
    event.preventDefault(); setBusy(true); setError("");
    try {
      const result = await pb.collection("users").authWithPassword(email, password);
      setUser(result.record);
      setPassword("");
      await refresh();
    } catch (e) {
      pb.authStore.clear(); setUser(null); setError(errorText(e));
    } finally { setBusy(false); }
  }

  async function createDraft(event) {
    event.preventDefault(); setBusy(true); setError("");
    try {
      if (!pb.authStore.isValid || !user) throw new Error("Sign in to create a draft");
      await pb.collection("posts").create({
        title: title.trim(), published: false, author: user.id,
      });
      setTitle("");
      await refresh();
    } catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  }

  async function signOut() {
    pb.authStore.clear(); setUser(null); setError("");
    try { await refresh(); } catch (e) { setError(errorText(e)); }
  }

  return <main className="shell">
    <header className="topbar">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcSet="/ruxbase-horizontal-white.svg" />
        <img className={dark ? "logo show-dark" : "logo show-light"} src={dark ? "/ruxbase-horizontal-white.svg" : "/ruxbase-horizontal-black.svg"} alt="Ruxbase" />
      </picture>
      <button className="theme" type="button" onClick={() => setDark(!dark)} aria-label={dark ? "Switch to light mode" : "Switch to dark mode"}>{dark ? "Light mode" : "Dark mode"}</button>
    </header>
    <section className="hero">
      <p className="eyebrow">RUXBASE / STARTER</p>
      <h1>Build without the overhead.</h1>
      <p className="subtitle">A small, real React application using Ruxbase and PostgreSQL-native records.</p>
      <p className="connection" aria-live="polite"><span className={online ? "dot online" : "dot"} />Backend {online ? "connected" : "not connected"}</p>
    </section>
    {error && <div role="alert" className="alert">{error}</div>}
    <div className="panels">
      <section className="panel">
        <h2>{user ? "Your account" : "Sign in"}</h2>
        {user ? <>
          <p data-testid="signed-in">Signed in as {user.email}</p>
          <button type="button" onClick={signOut}>Sign out</button>
          <form onSubmit={createDraft}>
            <label htmlFor="title">New draft</label>
            <input id="title" name="title" required minLength={2} maxLength={180} value={title} onChange={e => setTitle(e.target.value)} placeholder="Post title" />
            <button type="submit" disabled={busy || !title.trim()}>Create draft</button>
          </form>
        </> : <form onSubmit={signIn}>
          <label htmlFor="email">Email</label>
          <input id="email" type="email" autoComplete="username" required value={email} onChange={e => setEmail(e.target.value)} />
          <label htmlFor="password">Password</label>
          <input id="password" type="password" autoComplete="current-password" required value={password} onChange={e => setPassword(e.target.value)} />
          <button type="submit" disabled={busy}>Sign in</button>
        </form>}
      </section>
      <section className="panel">
        <div className="panel-header"><h2>Posts</h2><button type="button" onClick={() => {setError(""); refresh().catch(e => setError(errorText(e)));}}>Refresh</button></div>
        {posts.length === 0 && <p className="muted">No visible posts yet. Create the posts collection and sign in to write a draft.</p>}
        <ul data-testid="posts">{posts.map(post => <li key={post.id}><strong>{post.title}</strong><span>{post.published ? "Published" : "Draft"}</span></li>)}</ul>
      </section>
    </div>
    <footer>Development sample only · never expose database or admin credentials to the browser.</footer>
  </main>;
}
