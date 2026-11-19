# Restoring PocketBase Git ancestry

## Status and scope

The sole upstream is the official PocketBase repository. The reference tag is
v0.40.6 and its verified upstream commit is
`9d281ccaba65f2686eb7b0b614ccda72d1116a1b`. The published Ruxbase history was
initialized from snapshots; it does not currently establish a shared ancestor.

A successful CI run does not establish provenance. Do not mark the lockfile as
verified or assume all inherited changes have been integrated until the source
and resulting history have been reviewed.

## 1. Retrieve and inspect the reference

Run in an authorized local clone, with a clean working tree, after the
foundation documentation PR is merged. These commands do not alter main.

```sh
git clone https://github.com/AbdullahZaher/ruxbase.git
cd ruxbase
git remote add upstream https://github.com/pocketbase/pocketbase.git
git config remote.upstream.tagOpt --no-tags
git fetch upstream refs/tags/v0.40.6:refs/tags/upstream/v0.40.6
git fetch origin main
base=$(git rev-parse 'upstream/v0.40.6^{commit}')
test "$base" = 9d281ccaba65f2686eb7b0b614ccda72d1116a1b || exit 1
head=$(git rev-parse origin/main)
git diff --stat "$base" "$head"
git diff --name-status "$base" "$head"
git diff "$base" "$head" > ../ruxbase-baseline-review.patch
```

Review the full patch, inherited notices, PostgreSQL adaptations, migrations,
renames and tests. Confirm with the maintainer that this exact PocketBase
snapshot is the correct source baseline. If provenance cannot be established,
stop and continue with reviewed selective ports instead. Preserve an archive
of the review and record the exact source and target commits.

## 2. Create an ancestry candidate without rewriting history

Only after the review and explicit maintainer acceptance, create a documented
history-reconstruction commit with two parents: the current Ruxbase commit and
the verified upstream baseline. Use the exact existing Ruxbase tree. This
records an ancestry relationship prospectively; it does not pretend the
snapshot commits were originally authored on the upstream history.

```sh
test "$(git rev-parse origin/main)" = "$head" || exit 1
test -z "$(git status --porcelain)" || exit 1
git branch backup/pre-ancestry "$head"
tree=$(git rev-parse "$head^{tree}")
candidate=$(git commit-tree "$tree" -p "$head" -p "$base" \
  -m "Establish reviewed PocketBase v0.40.6 baseline ancestry")
test "$(git rev-parse "$candidate^{tree}")" = "$tree" || exit 1
git merge-base --is-ancestor "$head" "$candidate" || exit 1
git merge-base --is-ancestor "$base" "$candidate" || exit 1
git diff --exit-code "$head" "$candidate" || exit 1
git branch foundation/pocketbase-ancestry "$candidate"
git push origin refs/heads/backup/pre-ancestry refs/heads/foundation/pocketbase-ancestry
```

Do not force-push, delete the existing history, or push to the upstream
repository. If the branch names already exist, inspect them and select new
names rather than overwriting them.

## 3. Validate and integrate

- Confirm the candidate tree is byte-for-byte identical to the admitted
  starting tree before any documentation-only status update.
- Run the full CI on the candidate, including PostgreSQL 17/18, parity, strict
  mode, UI, SDK, build, lint and release snapshot.
- On an additional isolated branch, rehearse importing a real subsequent
  PocketBase tag; record conflicts, applicable changes and test evidence.
- Update the lockfile and NOTICE only after verification; record evidence and
  do not advance last_integrated_commit merely because ancestry exists.
- Integrate using a merge commit that preserves both parents (not squash or
  rebase). Do not merge if the main baseline has changed without reviewing
  the new diff.
- Recheck ancestry on the resulting main and run its CI. Keep the backup
  branch until the maintainer accepts the result.

The source code and existing Ruxbase commits remain intact. A regular Git
merge can then use the known common ancestor, but each upstream change still
requires selective review and PostgreSQL compatibility testing.
