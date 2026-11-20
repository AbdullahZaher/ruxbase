---
title: "Authentication and account lifecycle"
description: "Understand ordinary auth records, password login, refresh, OTP, MFA and account recovery."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Account identity
An auth collection defines allowed authentication methods and account settings. The default users collection is used in the SDK examples. Superusers are administrators, not normal application users. A frontend must never receive a superuser token.

## Password login and session
```js
const session = await pb.collection("users").authWithPassword(email, password);
const currentUser = session.record;
```
The parameters come from the user's UI, and pb is the client from [the JS guide](/ruxbase/integrations/javascript/). On later requests refresh via authRefresh; clear local state on logout. Server protection must validate the current authenticated request, not simply trust cached user data.

## Other account workflows
| Workflow | API suffix on the auth collection | Notes |
| --- | --- | --- |
| Discover methods | auth-methods | Check current configuration |
| Request OTP | request-otp | Follow configured delivery method |
| Complete OTP | auth-with-otp | Use issued challenge and password |
| OAuth2 | auth-with-oauth2 | Provider configuration and redirect flow required |
| Password recovery | request-password-reset, confirm-password-reset | Handle generic acknowledgement and validate returned outcome |
| Verification | request-verification, confirm-verification | Delivery is not guaranteed by request success |
| Change email | request-email-change, confirm-email-change | Requires appropriate authenticated flows |

MFA is configuration-dependent. An authentication challenge is not a completed login; inspect the scoped response and complete the required second method. See [account API examples](/ruxbase/reference/account-api/).

## Evidence
[Source: core/collection_model_auth_options.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/collection_model_auth_options.go), [Source: apis/record_auth_with_password.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/apis/record_auth_with_password.go), [Source: apis/record_auth_with_otp.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/apis/record_auth_with_otp.go), and [Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js). Password flow and refresh are exercised by the SDK suite. Provider/email delivery and MFA user journeys require additional environment-specific tests.
