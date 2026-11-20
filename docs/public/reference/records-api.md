---
title: "Record HTTP API examples"
description: "Interpret ordinary-record request paths, pagination and response shapes."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Record paths
Use a running Ruxbase origin and the current user's record token if required. With the example schema:
```http
GET /api/collections/posts/records?page=1&perPage=20&sort=id
```
The response has page, perPage, totalItems, totalPages and items. Counts may be -1 when skipped. Each item contains application-selected fields; do not generate a single universal record schema.

```http
POST /api/collections/posts/records
Content-Type: application/json
Authorization: USER_RECORD_TOKEN

{"title":"New article","author":"USER_RECORD_ID","published":false}
```
Replace symbolic values within your HTTP library; do not paste actual tokens into documentation. Success for create, read and update is 200; deletion returns 204. Use PATCH /api/collections/posts/records/RECORD_ID to update the permitted existing record.

## Error interpretation
Status depends on endpoint, validation and configured rules. A denied show/update/delete can return 404. Check the HTTP result before decoding; don't decode an empty body. Client retries of mutations require application-level reconciliation, not assumptions of idempotency.

## Evidence
[tested HTTP listing helper](/ruxbase/integrations/rest/); [scoped OpenAPI](/ruxbase/openapi.json); [Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js). The POST body is an illustrative schema-specific example; no universal idempotency contract is asserted.
