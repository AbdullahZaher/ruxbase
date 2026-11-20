# Ruxbase contributor and AI-agent instructions

Read [CONTRIBUTING.md](CONTRIBUTING.md), [branch strategy](docs/BRANCH-STRATEGY.md), [UPSTREAM.md](UPSTREAM.md), and the relevant source/tests before changing behavior. PocketBase is the sole ongoing upstream for inherited code; preserve Ruxbase PostgreSQL-specific behavior and compatibility evidence.

## Documentation
- Authoritative public guides are selected by docs/public/manifest.json. Keep original source paths; do not edit generated website directories or dist.
- Update public documentation when user-facing contracts change. State limitations, version and evidence. Never present planned features or development docs as production-certified.
- Do not put secrets, sensitive operational records or nonpublic information in this public repository or the documentation manifest.
- Run foundation, AI-context, clean build and static validation checks in website/README.md. Keep lockfile changes reviewed. Use only approved release tags for stable documentation.
- Do not use a successful docs build as application security certification, or deploy directly from a pull request.
