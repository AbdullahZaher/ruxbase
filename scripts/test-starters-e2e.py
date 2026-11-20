"""Full Phase 3 starter acceptance on an isolated GitHub Actions runner.

The test starts disposable Ruxbase+PostgreSQL via the checked-in Compose stack,
creates a scoped example schema/ordinary users and runs the generated React and
Next.js apps in real toolchains and Chromium. Does not touch any user database.
"""
import json
import os
import pathlib
import secrets
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUXBASE_STARTER_E2E") != "1":
    raise SystemExit("Starter E2E requires a disposable GitHub Actions runner")

ROOT = pathlib.Path(__file__).resolve().parents[1]
SERVER = "http://127.0.0.1:8090"
ADMIN = "ci-ruxbase-" + secrets.token_hex(5) + "@example.invalid"
PASSWORD = secrets.token_hex(24)
USERPASS = secrets.token_hex(16)
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def request(origin, method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"} if data is not None else {}
    if token:
        headers["Authorization"] = token
    req = urllib.request.Request(origin + path, data=data, headers=headers, method=method)
    try:
        with OPENER.open(req, timeout=12) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as e:
        raise AssertionError(f"{method} {path}: unexpected HTTP {e.code}") from e


def await_http(origin, path, timeout=180):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            status, data = request(origin, "GET", path)
            if status == 200:
                return data
        except (OSError, ValueError, AssertionError):
            pass
        time.sleep(2)
    raise RuntimeError("Server did not become ready at " + origin + path)


with tempfile.TemporaryDirectory(prefix="ruxbase-phase3-") as td:
    base = pathlib.Path(td)
    creds = base / ".env.quickstart"
    descriptor = os.open(creds, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(descriptor, "w") as writer:
        writer.write("POSTGRES_USER=ruxbase_local\nPOSTGRES_PASSWORD=" + PASSWORD + "\n")

    compose = [
        "docker", "compose", "--project-name", "ruxbase-phase3-ci",
        "--env-file", str(creds), "-f", str(ROOT / "compose.yaml"),
    ]

    def docker(*args, capture=False):
        result = subprocess.run(
            compose + list(args), cwd=ROOT, text=True, check=True,
            stdout=subprocess.PIPE if capture else None,
            stderr=subprocess.PIPE if capture else None,
        )
        return result.stdout if capture else None

    def provision():
        docker("exec", "-T", "ruxbase", "ruxbase", "superuser", "upsert",
               ADMIN, PASSWORD, "--dir=/app/ruxbase_data", "--dev=false", capture=True)
        _, auth = request(SERVER, "POST", "/api/collections/_superusers/auth-with-password",
                          {"identity": ADMIN, "password": PASSWORD})
        token = auth["token"]
        _, users = request(SERVER, "GET", "/api/collections/users", token=token)
        _, _ = request(SERVER, "POST", "/api/collections", {
            "name": "posts", "type": "base",
            "fields": [
                {"name": "title", "type": "text", "required": True},
                {"name": "published", "type": "bool"},
                {"name": "author", "type": "relation", "collectionId": users["id"], "maxSelect": 1},
                {"name": "created", "type": "autodate", "onCreate": True},
                {"name": "updated", "type": "autodate", "onCreate": True, "onUpdate": True}
            ],
            "listRule": "published = true || author = @request.auth.id",
            "viewRule": "published = true || author = @request.auth.id",
            "createRule": "@request.auth.id != '' && author = @request.auth.id",
            "updateRule": "author = @request.auth.id",
            "deleteRule": "author = @request.auth.id",
        }, token=token)
        accounts = []
        for name in ["alice", "bob"]:
            email = f"{name}_{secrets.token_hex(4)}@example.invalid"
            _, user = request(SERVER, "POST", "/api/collections/users/records", {
                "email": email, "password": USERPASS, "passwordConfirm": USERPASS, "name": name
            })
            _, session = request(SERVER, "POST", "/api/collections/users/auth-with-password",
                                  {"identity": email, "password": USERPASS})
            assert session["record"]["id"] == user["id"]
            accounts.append({"email": email, "password": USERPASS, "token": session["token"], "id": user["id"]})
        return accounts

    overlay_stack = compose + [
        "-f", str(ROOT / "starters/react/compose.yaml"),
        "-f", str(ROOT / "starters/nextjs/compose.yaml"),
    ]
    cleanup_stack = compose

    try:
        docker("up", "-d", "--build", "--wait", "--wait-timeout", "300")
        health = await_http(SERVER, "/api/health?ready")
        assert health["data"]["ready"] == {"database": True, "cluster": True}
        alice, bob = provision()
        print("Ruxbase/PostgreSQL disposable backend and two-user fixture: PASS", flush=True)

        # Exercise the exact deterministic generator, not a manually copied
        # frontend. The output directory is outside the repository.
        specs = [("react", "generated-react", 5173), ("nextjs", "generated-nextjs", 3000)]
        if os.environ.get("RUXBASE_AI_PLAN_FILE"):
            artifact = pathlib.Path(os.environ["RUXBASE_AI_PLAN_FILE"])
            subprocess.run(["node", str(ROOT / "scripts/eval-ai-projects.mjs"), "verify",
                            "--input", str(artifact)], check=True, cwd=ROOT)
            data = json.loads(artifact.read_text())
            specs = [(p["framework"], p["name"], 5173 if p["framework"] == "react" else 3000)
                     for p in data["plans"]]
        for framework, name, port in specs:
            project = base / name
            subprocess.run([
                "node", str(ROOT / "scripts/create-ruxbase-app.mjs"),
                framework, project.name, "--output", str(base),
            ], check=True, cwd=ROOT)
            assert project.is_dir()
            pkg = json.loads((project / "package.json").read_text())
            assert pkg["name"] == project.name
            subprocess.run(["npm", "ci", "--no-audit", "--no-fund"], cwd=project, check=True)
            subprocess.run(["npm", "audit", "--audit-level=high", "--omit=dev"], cwd=project, check=True)
            subprocess.run(["npm", "run", "build"], cwd=project, check=True, env={
                **os.environ, "NEXT_TELEMETRY_DISABLED": "1",
            })
            server_env = {
                **os.environ,
                "RUXBASE_INTERNAL_URL": SERVER,
                "NEXT_TELEMETRY_DISABLED": "1",
                "RUXBASE_APP_ORIGIN": "http://127.0.0.1:3000",
            }
            # Next.js production start exercises the built App Router bundle
            # without on-demand Turbopack recompilation during browser tests.
            # React stays on its Vite development proxy for SPA coverage.
            process = subprocess.Popen(
                ["npm", "run", "start" if framework == "nextjs" else "dev"], cwd=project, env=server_env,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
            try:
                origin = f"http://127.0.0.1:{port}"
                # The frontend must proxy to the real backend, not return a mock.
                health = await_http(origin, "/api/health?ready", 120)
                assert health["data"]["ready"]["database"]
                _, alice_auth = request(origin, "POST",
                                        "/api/collections/users/auth-with-password",
                                        {"identity": alice["email"], "password": alice["password"]})
                assert alice_auth["record"]["id"] == alice["id"]
                _, initial = request(origin, "GET", "/api/collections/posts/records")
                assert initial["items"] == []
                draft = "private-" + framework + "-" + secrets.token_hex(3)
                subprocess.run(
                    ["node", str(ROOT / "scripts/check-starter-browser.mjs"), origin, draft],
                    cwd=ROOT, check=True, env={
                        **server_env,
                        "CHROME_BIN": os.environ.get("CHROME_BIN", "google-chrome"),
                        "DOCS_BROWSER_NO_SANDBOX": "1",
                        "RUXBASE_TEST_EMAIL": alice["email"],
                        "RUXBASE_TEST_PASSWORD": alice["password"],
                    },
                )
                if framework == "nextjs":
                    subprocess.run(
                        ["python3", str(ROOT / "scripts/check-nextjs-session.py")],
                        check=True, cwd=ROOT, env={
                            **server_env,
                            "RUXBASE_TEST_ALICE_EMAIL": alice["email"],
                            "RUXBASE_TEST_ALICE_PASSWORD": alice["password"],
                            "RUXBASE_TEST_ALICE_ID": alice["id"],
                            "RUXBASE_TEST_BOB_EMAIL": bob["email"],
                            "RUXBASE_TEST_BOB_PASSWORD": bob["password"],
                            "RUXBASE_TEST_BOB_ID": bob["id"],
                        },
                    )
                _, own = request(origin, "GET", "/api/collections/posts/records",
                                 token=alice["token"])
                assert any(x["title"] == draft for x in own["items"]), "Owner draft not persisted"
                _, other = request(origin, "GET", "/api/collections/posts/records",
                                   token=bob["token"])
                assert all(x["title"] != draft for x in other["items"]), "Cross-user draft leakage"
                _, guest = request(origin, "GET", "/api/collections/posts/records")
                assert all(x["title"] != draft for x in guest["items"]), "Guest draft leakage"
                print(framework, "generated app build, same-origin proxy, Chromium interactions and tenant-like owner-rule denial PASS", flush=True)
            finally:
                import signal
                os.killpg(process.pid, signal.SIGTERM)
                process.wait(timeout=25)

        # Both Docker Compose overlays must render restricted host ports and
        # the internal-only backend target. Full generated-app runtime above
        # covers the framework itself; root Compose is already live-tested.
        for framework, port in [("react", 5173), ("nextjs", 3000)]:
            overlays = compose + ["-f", str(ROOT / "starters" / framework / "compose.yaml")]
            rendered = subprocess.run(overlays + ["config", "--format", "json"], cwd=ROOT,
                                      check=True, text=True, capture_output=True)
            model = json.loads(rendered.stdout)["services"]
            assert model[framework]["ports"][0]["host_ip"] == "127.0.0.1"
            assert str(model[framework]["ports"][0]["published"]) == str(port)
            assert model[framework]["environment"]["RUXBASE_INTERNAL_URL"] == "http://ruxbase:8090"
            assert model[framework]["depends_on"]["ruxbase"]["condition"] == "service_healthy"
        print("React and Next.js Docker overlays: restricted ports and private Ruxbase network PASS", flush=True)

        # Now start both development UIs as actual containers. This verifies
        # the Docker overlays, not just their static rendered configuration.
        cleanup_stack = overlay_stack
        subprocess.run(overlay_stack + [
            "up", "-d", "--wait", "--wait-timeout", "300", "react", "nextjs",
        ], cwd=ROOT, check=True)
        for framework, port in [("react", 5173), ("nextjs", 3000)]:
            origin = f"http://127.0.0.1:{port}"
            payload = await_http(origin, "/api/health?ready", 120)
            assert payload["data"]["ready"]["database"], framework + " container's proxy is not ready"
            with OPENER.open(origin + "/", timeout=12) as response:
                assert response.status == 200, framework + " frontend container not serving"
        print("Both Docker Compose frontend containers PASS: healthy, live /api proxy and browser entrypoint.", flush=True)
    finally:
        # Explicitly dispose of only this CI-owned project and all named
        # volumes, including frontend dependency volumes of the overlays.
        subprocess.run(cleanup_stack + ["down", "--volumes", "--remove-orphans"],
                       cwd=ROOT, check=True)
