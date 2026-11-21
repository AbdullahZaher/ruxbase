"""Exercise the documented compiled binary on a disposable CI runner.
No external database, no user data, no setup-token logs or artifacts.
"""
import json
import os
import pathlib
import re
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

root = pathlib.Path(__file__).resolve().parents[2]
doc = (root / "docs/public/quickstart.md").read_text()
assert "go build -o ruxbase ./examples/base\n./ruxbase serve --dev" in doc
env = {k: v for k, v in os.environ.items() if not k.startswith(("RUXBASE_", "PB_", "PG")) and k != "DATABASE_URL"}
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
base = "http://127.0.0.1:8090"
def get(path):
    with opener.open(base + path, timeout=3) as response:
        return response.status, response.read()
with tempfile.TemporaryDirectory(prefix="ruxbase-docs-quickstart-") as work:
    work = pathlib.Path(work)
    binary = work / "ruxbase"
    subprocess.run(["go", "build", "-o", str(binary), "./examples/base"], cwd=root, env=env, check=True)
    global_help = subprocess.check_output([str(binary), "--help"], cwd=work, env=env, text=True)
    serve_help = subprocess.check_output([str(binary), "serve", "--help"], cwd=work, env=env, text=True)
    refs = json.loads((root / "docs/contracts/reference.json").read_text())
    for flag in refs["cli"]:
        text = global_help if flag["scope"] == "global" else serve_help
        assert re.search(r"(?<![\w-])" + re.escape(flag["name"]) + r"(?![\w-])", text), flag["name"]
    # Do not accidentally validate a different process already on the expected port.
    try:
        get("/api/health")
    except (OSError, urllib.error.URLError):
        pass
    else:
        raise RuntimeError("Quickstart port already in use")
    hooks = work / "pb_hooks"
    hooks.mkdir()
    source = (root / "examples/docs-hooks/hello.pb.js").read_text()
    assert source.strip() in (root / "docs/public/extend-js/overview.md").read_text()
    (hooks / "hello.pb.js").write_text(source)
    process = subprocess.Popen([str(binary), "serve", "--dev", "--hooksDir", str(hooks), "--hooksWatch=false", "--publicDir", str(root / "examples/docs-posts")], cwd=work, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        deadline = time.monotonic() + 150
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise RuntimeError(f"Development binary exited: {process.returncode}")
            try:
                status, data = get("/api/health")
                result = json.loads(data)
                if status == 200 and result["code"] == 200 and result["data"] == {}:
                    break
            except (OSError, ValueError, urllib.error.URLError, KeyError):
                pass
            time.sleep(1)
        else:
            raise RuntimeError("Development liveness timed out")
        status, data = get("/_/")
        assert status == 200 and b"<html" in data.lower(), "Admin HTML unavailable"
        # Readiness is checked separately from liveness. Allow cluster startup.
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            try:
                status, data = get("/api/health?ready")
                ready = json.loads(data)["data"]["ready"]
                if status == 200 and ready == {"database": True, "cluster": True}:
                    break
            except (OSError, ValueError, KeyError, urllib.error.URLError):
                pass
            time.sleep(1)
        else:
            raise RuntimeError("Development readiness timed out")
        assert json.loads(get("/docs-example/js-hello")[1]) == {"message": "hello from JS"}
        assert b"Posts demo" in get("/")[1]
        assert b"export async function signIn" in get("/client.mjs")[1]
        print("JS hook and sample frontend static serving PASS.")
        print("Quickstart PASS: compiled binary, seven help flags, embedded database liveness/readiness, dashboard HTML.")
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=30)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()

# A separately compiled Go extension uses a fresh, disposable data directory.
with tempfile.TemporaryDirectory(prefix="ruxbase-docs-go-") as work:
    work = pathlib.Path(work)
    binary = work / "ruxbase-docs-go"
    example = root / "examples/docs-go/main.go"
    assert example.read_text().strip() in (root / "docs/public/extend-go/overview.md").read_text()
    subprocess.run(["go", "build", "-o", str(binary), "./examples/docs-go"], cwd=root, env=env, check=True)
    base = "http://127.0.0.1:8091"
    try:
        get("/api/health")
    except (OSError, urllib.error.URLError):
        pass
    else:
        raise RuntimeError("Go extension port already in use")
    process = subprocess.Popen([str(binary), "serve", "--dev", "--http=127.0.0.1:8091"], cwd=work, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        deadline = time.monotonic() + 150
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise RuntimeError(f"Go extension binary exited: {process.returncode}")
            try:
                status, data = get("/docs-example/hello")
                if status == 200 and json.loads(data) == {"message": "hello"}:
                    break
            except (OSError, ValueError, urllib.error.URLError):
                pass
            time.sleep(1)
        else:
            raise RuntimeError("Go extension route timed out")
        print("Go extension route PASS: exact documented source built and served.")
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=30)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
