# Development documentation publishing

The public site has not been deployed yet. The workflow is deliberately manual
and main-only, and does not create a release or promote development docs to stable.

Before first dispatch: complete the outstanding brand/content review and browser
checks, obtain maintainer approval of PR and release scope, confirm full CI on
the merged main commit, then set Settings > Pages > Source to GitHub Actions.
Restrict the github-pages environment to main and require a reviewer where
available. The workflow needs the full reviewed main SHA as input. It builds
from that SHA, tests the static artifact, and grants Pages/OIDC authority only
to the deployment job. No PR can deploy. The published version.json must match
the input and the smoke probes must pass. If the run fails, inspect the actual
deployment state rather than assuming rollback. Rebuilding a known-good source
and rollback rehearsal are still required before release certification.

Chromium smoke runs with no-sandbox only on disposable GitHub-hosted CI for
public static documentation, without production credentials. Local runs use
the sandbox by default. This is a functional smoke, not full accessibility,
visual equivalence, security or browser-matrix certification.

The integrated vector symbol and favicon are review candidates: owner visual approval is required before publication. Scalar distribution approval is additionally enforced by check-scalar-release-review.mjs; missing archive notices or unresolved bundle provenance must not be treated as cleared.
