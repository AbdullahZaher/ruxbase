---
title: "File HTTP API examples"
description: "Use multipart record writes and scoped file-token requests with correct access expectations."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## File endpoint
GET /api/files/COLLECTION/RECORD_ID/FILENAME returns file content. The filename is the server-returned stored name. Use SDK URL generation or encode each URL path segment in your client. Do not treat file content as JSON.

## Protected request
POST /api/files/token requires authenticated context and returns an object with token. Use the issued short-lived file token in the token query parameter of the intended protected-file URL. The token alone does not override configured file/record rules. Never share a token-bearing URL in public logs or cache it as public content.

Create or update file-bearing records with multipart FormData in your HTTP library. Do not manually assign a multipart boundary when the library generates it. See [upload example](/ruxbase/integrations/javascript/).

## Evidence
[Source: apis/file.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/apis/file.go), [Source: core/field_file.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/field_file.go); [Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js) tests ordinary upload/download. The independent protected-field access handler tests and deployment storage checks are separate from the generic SDK example. Review [files and storage](/ruxbase/concepts/files/) and [API inventory](/ruxbase/reference/api/).
