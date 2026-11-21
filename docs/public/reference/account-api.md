---
title: "Account HTTP API examples"
description: "Identify account request bodies, MFA boundaries and recovery scopes without exposing credentials."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Password and refresh
Use POST /api/collections/users/auth-with-password with JSON fields identity and password. A successful completed auth response contains token and record; a 401 with mfaId may denote a further challenge. Not every 401 is an MFA challenge. Do not expose passwords/tokens in logs or URLs.

```http
POST /api/collections/users/auth-refresh
Authorization: USER_RECORD_TOKEN
```
This requires authenticated context. Treat rejected refresh as an invalid session; the exact failure status depends on configuration and request.

## Recovery and methods
GET /api/collections/users/auth-methods describes configured methods. Password-reset request accepts an email; confirmation accepts the issued token and new password fields. Verify account recovery responses and delivery in a disposable mailbox before deploying. A successful 204 request acknowledgement does not prove that an account exists or that an email arrived.

OTP and OAuth2 are separate configured flows; follow the request schemas in [OpenAPI](/ruxbase/openapi.json), provider instructions and the installed SDK. MFA may require a second method using the issued challenge; a partial challenge is not a usable authenticated session.

## Evidence
[Source: apis/record_auth_with_password.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/apis/record_auth_with_password.go), [Source: apis/record_auth_password_reset_request.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/apis/record_auth_password_reset_request.go), [Source: apis/record_auth_password_reset_confirm.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/apis/record_auth_password_reset_confirm.go), [Source: apis/record_auth_with_otp.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/apis/record_auth_with_otp.go). Password and refresh exercised by SDK tests; account endpoints have repository handler tests, not a complete delivered-email/provider end-to-end certification. See [account concepts](/ruxbase/concepts/authentication/).
