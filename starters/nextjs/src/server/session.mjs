// Example server-only integration: a *new* PocketBase client for every request.
// Do not import into a Client Component or share this auth store across users.
import PocketBase, { BaseAuthStore } from "pocketbase";
import { NextResponse } from "next/server";

export const SESSION_COOKIE = "ruxbase_session";
export const defaultHeaders = {
  "Cache-Control": "private, no-store, max-age=0",
  "Pragma": "no-cache",
  "Vary": "Cookie",
};
export function json(body, status = 200) {
  return NextResponse.json(body, { status, headers: defaultHeaders });
}
function trustedOrigin(name, fallback) {
  const raw = process.env[name] || fallback;
  if (!raw) throw Error(name + " must be set to a trusted explicit origin");
  let url;
  try { url = new URL(raw); } catch { throw Error(name + " must be a valid origin"); }
  if (!["http:", "https:"].includes(url.protocol) || !url.hostname || url.username || url.password ||
      url.pathname !== "/" || url.search || url.hash || url.origin !== raw.replace(/\/$/, "")) {
    throw Error(name + " must be a URL origin without credentials, paths, queries or fragments");
  }
  if (process.env.NODE_ENV === "production" && name === "RUXBASE_APP_ORIGIN" &&
      url.protocol !== "https:" && !["127.0.0.1", "localhost"].includes(url.hostname)) {
    throw Error("Production RUXBASE_APP_ORIGIN requires HTTPS");
  }
  return url.origin;
}
export function backendUrl() {
  return trustedOrigin("RUXBASE_INTERNAL_URL", "http://127.0.0.1:8090");
}
export function applicationOrigin() {
  return trustedOrigin("RUXBASE_APP_ORIGIN", process.env.NODE_ENV === "production" ? "" : "http://127.0.0.1:3000");
}
export function isSafeMutation(request) {
  let expected;
  try { expected = applicationOrigin(); } catch { return false; }
  // Fail closed when Origin is absent, including for non-browser callers.
  const origin = request.headers.get("origin");
  const fetchSite = request.headers.get("sec-fetch-site");
  const type = request.headers.get("content-type") || "";
  // Next.js may canonicalize Request.url to its internal listener hostname
  // (e.g. localhost instead of 127.0.0.1). Trust neither that synthetic URL
  // nor forwarded-host headers for CSRF. Require the public, explicitly
  // configured origin AND the real incoming Host header to match.
  const host = request.headers.get("host");
  const expectedHost = new URL(expected).host;
  return origin === expected && host === expectedHost &&
    (!fetchSite || fetchSite === "same-origin" || fetchSite === "none") &&
    /^application\/json(?:\s*;|$)/i.test(type);
}
export async function parseJson(request) {
  const length = Number(request.headers.get("content-length") || 0);
  if (length > 8192) return null;
  try {
    const raw = await request.text();
    if (raw.length > 8192) return null;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    return parsed;
  } catch { return null; }
}
export function newClient() {
  const pb = new PocketBase(backendUrl(), new BaseAuthStore());
  pb.autoCancellation(false);
  // Never cache authenticated server-side SDK responses in Next's enhanced fetch.
  pb.beforeSend = (url, options) => ({ url, options: { ...options, cache: "no-store" } });
  return pb;
}
export function cookieOptions() {
  return {
    httpOnly: true, sameSite: "lax", path: "/", secure: process.env.NODE_ENV === "production",
    maxAge: 60 * 60,
  };
}
export function setSession(response, token) {
  response.cookies.set(SESSION_COOKIE, token, cookieOptions());
  return response;
}
export function clearSession(response) {
  response.cookies.set(SESSION_COOKIE, "", { ...cookieOptions(), maxAge: 0 });
  return response;
}
export async function verifiedSession(rawToken) {
  if (!rawToken || typeof rawToken !== "string" || rawToken.length > 8192) return null;
  const pb = newClient();
  try {
    pb.authStore.save(rawToken, null);
    if (!pb.authStore.isValid) return null;
    const result = await pb.collection("users").authRefresh({ requestKey: null });
    if (!result?.record?.id || !result?.token) return null;
    return { pb, user: result.record, token: result.token };
  } catch {
    pb.authStore.clear();
    return null;
  }
}
export function tokenFrom(request) {
  return request.cookies.get(SESSION_COOKIE)?.value || "";
}
export async function requireSession(request) {
  return verifiedSession(tokenFrom(request));
}
export function unauthorized() {
  return clearSession(json({ error: "Authentication required" }, 401));
}
