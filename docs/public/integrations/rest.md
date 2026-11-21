---
title: REST and HTTP integration
description: Connect any HTTP client to Ruxbase using scoped record endpoints and the Authorization token header.
sidebar:
  order: 5
---

**Development documentation.** Run examples against a disposable Ruxbase instance. Never embed a database connection string or a superuser credential in a client application.

## API base URL and health

Set BASE_URL to the server origin. Local examples assume a running Ruxbase at http://127.0.0.1:8090.

```sh
BASE_URL=http://127.0.0.1:8090
curl --fail-with-body "$BASE_URL/api/health"
```

Use [readiness and operations](/ruxbase/operations/overview/) for traffic routing. API responses are JSON except endpoints that use other representations or empty responses. Use the [OpenAPI contract](/ruxbase/openapi.json) for method, path and scoped schemas.

## Authentication header

Password authentication is POST /api/collections/users/auth-with-password with a JSON body containing identity and password. An ordinary authenticated response contains token and record. MFA can require an additional challenge; do not persist an incomplete-login state.

For subsequent record requests, use the Authorization header containing the record token itself. Do not assume a Bearer prefix is required. Keep tokens out of URLs and logs; use HTTPS outside local development.

## List posts with an HTTP client

Set up [the example app](/ruxbase/integrations/example-app/) first. A guest request omits the token and receives only records permitted by the list rule. A user request sends the user's own valid token.

```sh
curl --fail-with-body "$BASE_URL/api/collections/posts/records?page=1&perPage=50&sort=views"
```

The following JavaScript uses only standard URL and fetch APIs, not the SDK. This function is also imported by the repository's compatibility suite for guest and authenticated record-list checks.

```js
export async function listVisiblePosts(baseUrl, collectionName, token) {
    const url = new URL(
        "/api/collections/" + encodeURIComponent(collectionName) + "/records",
        baseUrl,
    );
    url.searchParams.set("page", "1");
    url.searchParams.set("perPage", "50");
    url.searchParams.set("sort", "views");
    const headers = token ? { Authorization: token } : {};
    const response = await fetch(url, { headers });
    if (!response.ok) throw new Error("List request failed: " + response.status);
    return response.json();
}
```

## Create and update with an HTTP library

POST /api/collections/posts/records creates a record. Send a JSON object such as {"title":"First article","author":"USER_RECORD_ID","published":false} with Content-Type application/json and the authenticated ordinary record token. Substitute the actual current user's record ID. GET /api/collections/posts/records/RECORD_ID reads one; PATCH on that path modifies writable fields; DELETE removes a permitted record and normally returns 204. Multipart requests are required for typical file uploads; consult the [JavaScript guide](/ruxbase/integrations/javascript/) and [API reference](/ruxbase/reference/api/).

Do not assume 404 always means the record is absent: API rules can conceal inaccessible records. Validate request errors by status and documented response shape rather than matching server-specific database text. Do not retry writes blindly; handle network ambiguity according to your application contract.

## Evidence and limits

See [API coverage](/ruxbase/api-coverage.json), [OpenAPI](/ruxbase/openapi.json) and [Ruxbase JS SDK test suite](https://github.com/AbdullahZaher/ruxbase/blob/d03ba38b8fa2063cd1479c9198c44de21a16de93/tests/sdk/js/sdk.test.js). The shared HTTP listing function tests the request header, guest filtering and authenticated visibility. The scope does not establish every schema or language-specific client behavior, and the documented REST paths do not define application-specific collection schemas.
