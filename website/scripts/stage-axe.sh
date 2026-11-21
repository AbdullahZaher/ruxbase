#!/usr/bin/env bash
# Download the test-only axe bundle; compare its bytes with upstream tag v4.11.1 sri-history.json.
set -euo pipefail
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
npm pack axe-core@4.11.1 --ignore-scripts --json --pack-destination "$tmp" > "$tmp/pack.json"
python3 - "$tmp" "$1" <<'PY'
import base64, hashlib, json, pathlib, sys, tarfile
tmp = pathlib.Path(sys.argv[1])
info = json.loads((tmp / 'pack.json').read_text())[0]
assert info['name'] == 'axe-core' and info['version'] == '4.11.1'
with tarfile.open(tmp / pathlib.Path(info['filename']).name, 'r:gz') as archive:
    member = archive.getmember('package/axe.min.js')
    assert member.isfile() and member.size < 2 * 1024 * 1024
    with archive.extractfile(member) as f:
        data = f.read()
assert 'sha256-' + base64.b64encode(hashlib.sha256(data).digest()).decode() == 'sha256-fb+r38YGKTbXnIc927X4EaEhn8o5KL2Myd2B8eZfRyA='
pathlib.Path(sys.argv[2]).write_bytes(data)
print('Verified axe-core 4.11.1 upstream bundle digest.')
PY
