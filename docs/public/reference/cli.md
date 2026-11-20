---
title: CLI reference
description: Core startup and serve flags in the Ruxbase binary.
---

**Development documentation.** Scoped reference, not a certified production release.

Use `./ruxbase --help` and `./ruxbase serve --help` to inspect the installed binary. Plugin-specific flags and other commands are outside this initial reference.

## Global flags

| Name | Default | Description |
| --- | --- | --- |
| `--dir` | ruxbase_data relative to executable | Data directory. |
| `--dev` | false for compiled binary | Development logs; embedded PostgreSQL if DATABASE_URL is absent. |
| `--encryptionEnv` | none | Name of environment variable holding the 32-character settings encryption key. |
| `--queryTimeout` | 30 seconds | Default SELECT query timeout. |

## Serve flags

| Name | Default | Description |
| --- | --- | --- |
| `--http` | 127.0.0.1:8090 without domain arguments | HTTP listener address; domain arguments change the default. |
| `--https` | unset without domain arguments | HTTPS listener address. |
| `--origins` | * | Allowed CORS origins; review and restrict for the intended application. |

See [Quickstart](/ruxbase/getting-started/quickstart/).
