---
title: Mobile integration: React Native and Flutter
description: Connect mobile clients to a reachable Ruxbase URL and plan secure auth persistence and realtime.
sidebar:
  order: 6
---

**Development documentation.** Run examples against a disposable Ruxbase instance. Never embed a database connection string or a superuser credential in a client application.

## Mobile server address

The mobile device or emulator needs a reachable server URL. The device's localhost is not necessarily the computer running Ruxbase. Configure a development-only reachable address appropriate to the device or emulator, keep the development server isolated from untrusted networks, and use HTTPS with a trusted certificate for deployed applications. Do not disable TLS validation to make a connection work.

## React Native

Install the JavaScript package from [the SDK guide](/ruxbase/integrations/javascript/). The basic auth and collection calls use the same JavaScript SDK methods.

To persist login across launches, supply an AsyncAuthStore backed by appropriate application storage. A non-browser runtime may need an EventSource polyfill for realtime. Select maintained libraries compatible with your actual React Native version, protect tokens with OS-backed secure storage when feasible, and clear persisted authentication on logout. See [upstream mobile setup](https://pocketbase.io/docs/how-to-use/) for runtime-specific APIs.

## Flutter or Dart

Ruxbase's compatibility target includes the PocketBase REST contract. Use the official upstream Dart SDK package pocketbase, choose and lock a compatible version in your application, and test against your Ruxbase server. No Ruxbase-specific Dart SDK package is required.

This is an illustrative connection and ordinary-user login pattern, not an executed Dart test:

```dart
import 'package:pocketbase/pocketbase.dart';

Future<void> signIn(String serverUrl, String email, String password) async {
  final pb = PocketBase(serverUrl);
  await pb.collection('users').authWithPassword(email, password);
  // Use this authenticated client for the current user's requests.
}
```

In a real application retain the client for the user session, choose protected token persistence and handle the configured authentication method. Do not pass superuser credentials to the device. See the [Dart SDK repository](https://github.com/pocketbase/dart-sdk).

## Validation checklist

- Verify password login, session restore and logout with a test account.
- Test permitted and denied records using two separate users.
- Verify file uploads, app background/foreground transitions, network reconnect and SSE subscription cleanup.
- Exercise the exact supported device/emulator OS and SDK versions; do not assume a browser test establishes mobile compatibility.

## Evidence and limitations

The Ruxbase repository runs the pinned JS SDK against a disposable server. It does not currently run React Native device tests or a Dart SDK compatibility suite. Dart and mobile examples are upstream-compatible starting points requiring application-level verification, not certified integrations. See [compatibility](/ruxbase/migration/compatibility/) and [realtime](/ruxbase/guides/realtime/).
