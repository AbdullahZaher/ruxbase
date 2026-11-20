#!/usr/bin/env bash
# Candidate only: registry integrity is recorded for review, not yet committed.
set -euo pipefail
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
npm pack @scalar/api-reference@1.69.2 --ignore-scripts --json --pack-destination "$tmp" > "$tmp/pack.json"
python3 - "$tmp" <<'PY'
import base64, hashlib, json, pathlib, shutil, sys, tarfile
tmp = pathlib.Path(sys.argv[1])
info = json.loads((tmp / 'pack.json').read_text())[0]
assert info['name'] == '@scalar/api-reference' and info['version'] == '1.69.2'
archive = tmp / pathlib.Path(info['filename']).name
digest = 'sha512-' + base64.b64encode(hashlib.sha512(archive.read_bytes()).digest()).decode()
assert digest == info['integrity']
print('SCALAR-INTEGRITY', digest)
target = pathlib.Path('dist/api-viewer')
target.mkdir(exist_ok=False)
vendor = target / 'vendor'
vendor.mkdir()
total = 0
with tarfile.open(archive, 'r:gz') as tar:
    for m in tar:
        prefix = 'package/dist/browser/'
        if not m.name.startswith(prefix) or not m.isfile() or m.name.endswith('.map'):
            continue
        rel = pathlib.PurePosixPath(m.name[len(prefix):])
        if rel.is_absolute() or '..' in rel.parts or not rel.parts:
            raise ValueError('unexpected package path')
        total += m.size
        if total > 30 * 1024 * 1024:
            raise ValueError('candidate bundle too large')
        dest = vendor.joinpath(*rel.parts)
        dest.parent.mkdir(parents=True, exist_ok=True)
        with tar.extractfile(m) as src, dest.open('xb') as out:
            shutil.copyfileobj(src, out)
assert (vendor / 'standalone.js').is_file() or (vendor / 'standalone.esm.js').is_file()
for name in ['index.html', 'viewer.js']:
    shutil.copyfile(pathlib.Path('scalar-candidate') / name, target / name)
(target / 'provenance.json').write_text(json.dumps({'package': info['name'], 'version': info['version'], 'integrity': digest, 'scope': 'candidate-only'}, indent=2) + '\n')
print('Scalar candidate staged:', total, 'bytes')
PY
