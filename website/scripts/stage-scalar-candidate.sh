#!/usr/bin/env bash
# Development viewer: exact version and archive integrity pinned after tested CI.
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
assert digest == info['integrity'] == 'sha512-dh72RlSKRfjk+wTfplO1oNLQBGKmczWxi+zoI553pgPA7Zer03iBf084qqGoIwwSA+odO2oXIxwvcXVHTWRE/Q=='
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
# Preserve publisher-provided license/notice files and exact package metadata.
with tarfile.open(archive, 'r:gz') as tar:
    pkg = json.load(tar.extractfile('package/package.json'))
    assert pkg['name'] == info['name'] and pkg['version'] == info['version']
    legal = target / 'legal'
    legal.mkdir()
    notices = []
    for m in tar:
        if not m.isfile() or not m.name.startswith('package/'):
            continue
        name = pathlib.PurePosixPath(m.name).name
        if not name.upper().startswith(('LICENSE', 'LICENCE', 'NOTICE', 'COPYING', 'THIRD-PARTY', 'THIRD_PARTY')):
            continue
        rel = pathlib.PurePosixPath(m.name[len('package/'):])
        assert not rel.is_absolute() and '..' not in rel.parts and m.size <= 4 * 1024 * 1024
        dest = legal.joinpath(*rel.parts)
        dest.parent.mkdir(parents=True, exist_ok=True)
        with tar.extractfile(m) as src, dest.open('xb') as out:
            shutil.copyfileobj(src, out)
        notices.append(str(rel))
    (legal / 'package-review.json').write_text(json.dumps({
        'name': pkg['name'], 'version': pkg['version'], 'declaredLicense': pkg.get('license'),
        'declaredDependencies': pkg.get('dependencies', {}), 'declaredOptionalDependencies': pkg.get('optionalDependencies', {}),
        'declaredPeerDependencies': pkg.get('peerDependencies', {}),
        'includedNotices': sorted(notices),
        'reviewStatus': 'inventory-only',
        'limitations': 'Package declarations are not an SBOM of the bundled standalone JS. Transitive license and advisory review remains required.'
    }, indent=2) + '\n')
    assert pkg.get('license'), 'Missing package license metadata'
    print('Scalar declared license:', pkg['license'], '; included legal files:', len(notices))
assert (vendor / 'standalone.js').is_file() or (vendor / 'standalone.esm.js').is_file()
print('Scalar entrypoints:', [p.name for p in vendor.glob('standalone*')])
for name in ['index.html', 'viewer.js']:
    shutil.copyfile(pathlib.Path('scalar-candidate') / name, target / name)
(target / 'provenance.json').write_text(json.dumps({'package': info['name'], 'version': info['version'], 'integrity': digest, 'scope': 'development-viewer', 'files': {str(p.relative_to(target)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(target.rglob('*')) if p.is_file()}}, indent=2) + '\n')
print('Scalar viewer staged:', total, 'bytes')
PY
