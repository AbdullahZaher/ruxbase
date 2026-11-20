---
title: "Files and storage access"
description: "Store record attachments, handle protected files and choose local or shared storage."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Upload and retrieve
Configure a file field with size, cardinality and accepted formats suitable for the application. Use FormData for uploads and the returned stored filename rather than the original name. See [the tested multipart pattern](/ruxbase/integrations/javascript/).

A public file URL is not a secret. For protected fields, the server checks the file token and relevant record visibility. Authenticate an ordinary user, obtain a short-lived file token via POST /api/files/token and use the token only for the intended protected file request. Keep token-bearing URLs out of logs, analytics and shared caches. See [file API contract](/ruxbase/reference/file-api/).

## Storage topology
Local storage is appropriate only when its lifecycle and availability fit the deployment. Multi-instance operation needs shared S3-compatible storage; nodes cannot share independent local files. A database backup alone does not cover all external object storage. Rehearse restore of both the database and files.

## Evidence
[Source: core/field_file.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/field_file.go), [Source: apis/file.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/apis/file.go), [Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js); [clustering](/ruxbase/operations/clustering/). Existing SDK suite tests ordinary multipart upload and download, not a complete protected-file or S3 provider matrix.
