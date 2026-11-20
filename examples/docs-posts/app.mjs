import { signIn, createDraft, readPosts } from "./client.mjs";
const base = location.origin;
const status = document.querySelector("#status");
const items = document.querySelector("#items");
let session = null;
let busy = false;
async function action(fn) {
    if (busy) return;
    busy = true;
    try { await fn(); status.textContent = session ? "Signed in" : "Signed out"; }
    catch { status.textContent = "Request failed; check login, schema and rules."; }
    finally { busy = false; }
}
async function refresh() {
    const result = await readPosts(base, "posts", session);
    items.replaceChildren(...result.items.map(post => {
        const li = document.createElement("li");
        li.textContent = post.title;
        return li;
    }));
}
document.querySelector("#login").addEventListener("submit", event => {
    event.preventDefault();
    const form = event.currentTarget;
    void action(async () => {
        const input = new FormData(form);
        session = null;
        items.replaceChildren();
        try { session = await signIn(base, input.get("email"), input.get("password")); }
        finally { form.reset(); }
        await refresh();
    });
});
document.querySelector("#post").addEventListener("submit", event => {
    event.preventDefault();
    const form = event.currentTarget;
    void action(async () => {
        if (!session) throw new Error("Sign in first");
        await createDraft(base, "posts", session, new FormData(form).get("title"));
        form.reset();
        await refresh();
    });
});
document.querySelector("#refresh").addEventListener("click", () => void action(refresh));
document.querySelector("#logout").addEventListener("click", () => void action(async () => {
    session = null;
    items.replaceChildren();
    await refresh();
}));
void action(refresh);
