# Ruxbase branch strategy

## Model

Use trunk-based development with temporary wave integration branches. There is
no permanent develop branch: main remains the canonical integration trunk.
Production releases are separately certified, immutable version tags; a green
main alone is not release approval.

| Ref | Purpose | Source / target |
| --- | --- | --- |
| main | Canonical reviewed integration | PR only |
| wave/NN-slug | Temporary wave integration | Created from main; PR to main after wave gates |
| feat/wNN-mNN-slug | One bounded engineering increment | Active wave; PR to that wave |
| fix/slug | Defect or compatibility fix | Affected integration branch; PR back to it |
| chore/slug | Repository/tooling maintenance | main or affected wave; PR back |
| integration/pocketbase-vX.Y.Z | Reviewed selective upstream update | main or active wave; isolated PR |
| upstream/pocketbase-vX.Y.Z | Immutable official reference, when available | Official PocketBase commit only |
| release/vX.Y.Z | Optional stabilization branch | Certified main baseline |
| hotfix/vX.Y.Z-slug | Urgent released-version fix | Released tag; integrate into main and active waves |
| backup/* | Preservation/evidence refs | Never normal development targets |

Existing foundation/* refs remain historical evidence. Do not rename or delete
them until the maintainer verifies all open issues and evidence links. The
import helper is not an integration branch.

## Integration rules

1. Record the exact base commit/tree and issue/milestone in every engineering PR.
2. Use focused feature branches; no direct pushes to main or wave integration refs.
3. Keep wave PRs draft until executable evidence and independent review exist.
4. Require full CI for the exact candidate, plus relevant benchmark, upstream
   inventory, PostgreSQL parity and security checks. Check again after integration.
5. Use merge commits when preserving the review or upstream history matters.
   Never squash or rebase an ancestry restoration. Never force-update shared refs.
6. Integrate main changes into active waves through reviewed PRs; re-run gates.
7. Keep official PocketBase as sole upstream. No automatic upstream merges.
8. Do not publish a version tag or deploy production without release approval.
9. Owner configures rulesets for main and wave/* requiring PR review and required
   checks, blocking force pushes and deletion. This document does not enforce
   them: until owner configuration is verified, these are procedural controls.

## First application

Wave 01 uses wave/01-secure-platform-kernel. The first increment is
feat/w01-m01-runtime-role-preflight, based on the exact same starting commit and
targeting the wave branch. [Wave 00 issue #4](https://github.com/AbdullahZaher/ruxbase/issues/4)
is closed. The master checklist tracks the remaining Wave 00 review and
certification gates. Production authorization remains NO-GO.

The wave integration branch is removed only after its complete reviewed
integration, post-merge CI and evidence retention are verified.
