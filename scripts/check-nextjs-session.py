"""Negative and positive Next.js App Router cookie-session integration.

Runs against the *built*, disposable Next.js app and real Ruxbase/PostgreSQL.
Ordinary test passwords are passed through inherited process environment.
"""
import concurrent.futures
import http.client
import json
import os
from urllib.parse import urlparse

ORIGIN = os.environ["RUXBASE_APP_ORIGIN"]
ALICE = {"email": os.environ["RUXBASE_TEST_ALICE_EMAIL"], "password": os.environ["RUXBASE_TEST_ALICE_PASSWORD"], "id": os.environ["RUXBASE_TEST_ALICE_ID"]}
BOB = {"email": os.environ["RUXBASE_TEST_BOB_EMAIL"], "password": os.environ["RUXBASE_TEST_BOB_PASSWORD"], "id": os.environ["RUXBASE_TEST_BOB_ID"]}
url = urlparse(ORIGIN)
assert url.hostname == "127.0.0.1" and url.port == 3000, "Session test must target local temporary Next.js"

def call(method, path, body=None, cookie=None, origin=ORIGIN, fetch_site="same-origin", content_type="application/json"):
    headers = {}
    if origin is not None: headers["Origin"] = origin
    if fetch_site is not None: headers["Sec-Fetch-Site"] = fetch_site
    if cookie: headers["Cookie"] = cookie
    data = json.dumps(body).encode() if body is not None else None
    if data is not None:
        headers["Content-Type"] = content_type
    conn = http.client.HTTPConnection(url.hostname, url.port, timeout=15)
    try:
        conn.request(method, path, body=data, headers=headers)
        res = conn.getresponse()
        raw = res.read()
        return res.status, dict((k.lower(), v) for k,v in res.getheaders()), raw
    finally:
        conn.close()

def as_json(result):
    return json.loads(result[2])

def cookie_value(resp):
    header = resp[1].get("set-cookie", "")
    assert header.lower().startswith("ruxbase_session="), "Session cookie missing"
    return header.split(";", 1)[0]

def assert_no_store(resp):
    assert "no-store" in resp[1].get("cache-control", ""), "Authenticated response is cacheable"
    assert "cookie" in resp[1].get("vary", "").lower(), "Missing Vary: Cookie"

# Reject cross-site, missing-origin and non-JSON POSTs, even on unauthenticated login.
payload = {"email": ALICE["email"], "password": ALICE["password"]}
for origin, site, typ in [(None, None, "application/json"),
                          ("https://evil.example", "cross-site", "application/json"),
                          (ORIGIN, "cross-site", "application/json"),
                          (ORIGIN, "same-origin", "text/plain")]:
    resp = call("POST", "/api/session/login", payload, origin=origin, fetch_site=site, content_type=typ)
    assert resp[0] == 403, ("CSRF must fail closed", resp[0])
assert call("POST", "/api/session/login", {"email": "bad", "password": "wrong"})[0] == 401
assert call("POST", "/api/session/login", {"email": ALICE["email"], "password": "x" * 9000})[0] == 400

cookies = {}
for person in [ALICE, BOB]:
    resp = call("POST", "/api/session/login", {"email": person["email"], "password": person["password"]})
    assert resp[0] == 200, ("Login failed", resp[0])
    assert as_json(resp)["user"]["id"] == person["id"]
    assert "token" not in as_json(resp), "Session token leaked in JSON response"
    header = resp[1]["set-cookie"].lower()
    assert "httponly" in header and "samesite=lax" in header and "secure" in header
    assert "path=/" in header
    cookies[person["id"]] = cookie_value(resp)

a, b = cookies[ALICE["id"]], cookies[BOB["id"]]
assert a != b, "Separate users received the same session cookie"
for cookie, person in [(a, ALICE), (b, BOB)]:
    resp = call("GET", "/api/session/me", cookie=cookie)
    assert resp[0] == 200
    assert as_json(resp)["user"]["id"] == person["id"]
    assert_no_store(resp)

# A valid signed token must be refreshed against the actual backend; forged,
# expired and absent cookies never authorize protected API or server rendering.
for cookie in [None, "ruxbase_session=garbage", "ruxbase_session=eyJhbGciOiJub25lIn0.e30.foo"]:
    assert call("GET", "/api/session/me", cookie=cookie)[0] == 401
    assert call("GET", "/api/session/posts", cookie=cookie)[0] == 401
    redirect = call("GET", "/protected", cookie=cookie)
    assert redirect[0] in (303, 307, 308), redirect[0]

def current_user(cookie):
    result = call("GET", "/api/session/me", cookie=cookie)
    return (result[0], as_json(result).get("user", {}).get("id"))
with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
    inputs = [a, b, None, "ruxbase_session=garbage"] * 8
    actual = list(pool.map(current_user, inputs))
    assert actual == [(200, ALICE["id"]), (200, BOB["id"]), (401, None), (401, None)] * 8, "Request auth leaked between concurrent users"

draft = "server-batch02-owned-draft"
for headers in [(None, None), ("https://evil.example", "cross-site")]:
    assert call("POST", "/api/session/posts", {"title": draft}, a, origin=headers[0], fetch_site=headers[1])[0] == 403
assert call("POST", "/api/session/posts", {"title": "a"}, a)[0] == 400
assert call("POST", "/api/session/posts", {"title": draft}, None)[0] == 401
created = call("POST", "/api/session/posts",
               {"title": draft, "author": BOB["id"], "published": True}, a)
assert created[0] == 201, ("Create draft",created[0])
post = as_json(created)
assert post["author"] == ALICE["id"] and post["published"] is False, "Client forged ownership or publication"

owner = call("GET", "/api/session/posts", cookie=a)
other = call("GET", "/api/session/posts", cookie=b)
assert owner[0] == other[0] == 200
assert any(x["id"] == post["id"] for x in as_json(owner)["items"])
assert all(x["id"] != post["id"] for x in as_json(other)["items"]), "Cross-user private draft leaked"
assert_no_store(owner)
ssr_a, ssr_b = call("GET", "/protected", cookie=a), call("GET", "/protected", cookie=b)
assert ssr_a[0] == ssr_b[0] == 200
assert draft.encode() in ssr_a[2] and draft.encode() not in ssr_b[2], "Server-rendered content leaked"
assert ALICE["email"].encode() in ssr_a[2] and ALICE["email"].encode() not in ssr_b[2]
assert a.split("=",1)[1].encode() not in ssr_a[2], "Cookie token leaked in protected HTML"

assert call("POST", "/api/session/logout", {}, a, origin="https://evil.example")[0] == 403
logout = call("POST", "/api/session/logout", {}, a)
assert logout[0] == 200 and "max-age=0" in logout[1]["set-cookie"].lower()
assert_no_store(logout)
print("Next.js SSR sessions PASS: strict Origin+Fetch-Site+JSON CSRF, HttpOnly secure cookies, backend refresh, request isolation 32 parallel calls, owner draft spoof defense, no-store responses, protected HTML, logout.", flush=True)
