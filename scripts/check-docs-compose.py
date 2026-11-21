"""Run the documented Docker Compose Quickstart in a disposable CI project.

Requires RUXBASE_COMPOSE_CI=1 on an isolated runner with Docker installed.
No existing user containers or volumes may be reused or deleted.
"""
import json
import os
import pathlib
import secrets
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

if os.environ.get("RUXBASE_COMPOSE_CI") != "1" or os.environ.get("GITHUB_ACTIONS") != "true":
    raise SystemExit("Docker Compose E2E is restricted to isolated GitHub Actions runners")

ROOT = pathlib.Path(__file__).resolve().parents[1]
QUICKSTART = (ROOT / "docs/public/quickstart.md").read_text()
assert "docker compose --env-file .env.quickstart up -d --build --wait" in QUICKSTART
assert (ROOT / "compose.yaml").is_file(), "Default compose.yaml missing"

opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
server = "http://127.0.0.1:8090"


def request(path):
    with opener.open(server + path, timeout=4) as response:
        return response.status, response.read()


def await_ready(timeout=150):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            code, payload = request("/api/health?ready")
            data = json.loads(payload)
            if code == 200 and data.get("data", {}).get("ready") == {"database": True, "cluster": True}:
                return
        except (OSError, ValueError, urllib.error.URLError):
            pass
        time.sleep(2)
    raise RuntimeError("Docker Compose Ruxbase readiness timed out")


with tempfile.TemporaryDirectory(prefix="ruxbase-docs-compose-") as work:
    credentials = pathlib.Path(work) / ".env.quickstart"
    fd = os.open(credentials, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as stream:
        stream.write("POSTGRES_USER=ruxbase_local\nPOSTGRES_PASSWORD=" + secrets.token_hex(24) + "\n")

    # This project name is reserved for disposable GitHub Actions runners only.
    cmd = [
        "docker", "compose", "--project-name", "ruxbase-docs-ci",
        "--env-file", str(credentials), "-f", str(ROOT / "compose.yaml"),
    ]

    def compose(*args, capture=False):
        run = subprocess.run(
            cmd + list(args), cwd=ROOT, check=True, text=True,
            stdout=subprocess.PIPE if capture else None,
            stderr=subprocess.PIPE if capture else None,
        )
        return run.stdout if capture else None

    # Inspect the rendered Compose model without printing passwords or URLs.
    compose("config", "--quiet")
    config = json.loads(compose("config", "--format", "json", capture=True))
    postgres = config["services"]["postgres"]
    app = config["services"]["ruxbase"]
    assert not postgres.get("ports"), "PostgreSQL must not publish a host port"
    ports = app.get("ports", [])
    assert len(ports) == 1 and ports[0]["host_ip"] == "127.0.0.1" and str(ports[0]["published"]) == "8090"
    assert "DATABASE_URL" in app["environment"], "Expected app database configuration"
    assert app["depends_on"]["postgres"]["condition"] == "service_healthy"
    assert app.get("healthcheck") and postgres.get("healthcheck")
    assert app.get("volumes") and postgres.get("volumes")

    try:
        compose("up", "-d", "--build", "--wait", "--wait-timeout", "300")
        await_ready()

        code, body = request("/_/")
        assert code == 200 and b"<html" in body.lower(), "Admin dashboard unavailable"
        code, body = request("/api/health")
        assert code == 200 and json.loads(body)["code"] == 200

        vector = compose(
            "exec", "-T", "postgres", "psql", "-X", "-v", "ON_ERROR_STOP=1",
            "-U", "ruxbase_local", "-d", "ruxbase", "-Atc",
            "SELECT default_version FROM pg_available_extensions WHERE name='vector'",
            capture=True,
        )
        assert vector.strip(), "pgvector extension is not available in PostgreSQL 18"

        # This schema exists only in the CI-owned disposable database.
        compose(
            "exec", "-T", "postgres", "psql", "-X", "-v", "ON_ERROR_STOP=1",
            "-U", "ruxbase_local", "-d", "ruxbase", "-c",
            "CREATE SCHEMA docs_compose_ci; "
            "CREATE TABLE docs_compose_ci.sentinel (id int PRIMARY KEY, value text NOT NULL); "
            "INSERT INTO docs_compose_ci.sentinel VALUES (1, 'persisted');",
            capture=True,
        )

        # Down + up replaces containers while leaving both named volumes intact.
        compose("down", "--remove-orphans")
        compose("up", "-d", "--wait", "--wait-timeout", "300")
        await_ready()
        value = compose(
            "exec", "-T", "postgres", "psql", "-X", "-v", "ON_ERROR_STOP=1",
            "-U", "ruxbase_local", "-d", "ruxbase", "-Atc",
            "SELECT value FROM docs_compose_ci.sentinel WHERE id=1",
            capture=True,
        )
        assert value.strip() == "persisted", "Database did not survive container recreation"
        assert request("/_/")[0] == 200, "Admin dashboard unavailable after restart"
        print("Docker Compose Quickstart PASS: complete image, private PostgreSQL 18, pgvector, health/readiness, dashboard, persistence.")
    finally:
        # Never run this on a non-ephemeral host; the isolated test data is
        # intentionally destroyed here, unlike ordinary documented down.
        compose("down", "--volumes", "--remove-orphans")
