---
title: "React and browser SPA integration"
description: "Initialize the SDK once per browser session and manage component lifecycle in React."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Prerequisites
Install pocketbase@0.28.1 and set the Ruxbase server origin as public application configuration. Configure the posts schema and rules from [the example app](/ruxbase/integrations/example-app/). An origin is public; administrative credentials and database URLs are not.

## React example
The following component is illustrative JSX. It uses the tested getList SDK operation; it has not been run in a React toolchain.
```jsx
import { useEffect, useState } from "react";
import PocketBase from "pocketbase";

const pb = new PocketBase("http://127.0.0.1:8090");

export function PostList() {
    const [items, setItems] = useState([]);
    const [error, setError] = useState("");
    useEffect(() => {
        let active = true;
        pb.collection("posts").getList(1, 20, { sort: "id", requestKey: null })
            .then(result => { if (active) setItems(result.items); })
            .catch(() => { if (active) setError("Unable to load posts"); });
        return () => { active = false; };
    }, []);
    if (error) return <p>{error}</p>;
    return <ul>{items.map(post => <li key={post.id}>{post.title}</li>)}</ul>;
}
```
Use JSX text rendering, not unsanitized HTML. For authentication, refresh app state when the current session changes, handle configured MFA, and remove realtime subscriptions on cleanup. Vue and Svelte can use the same SDK in client-only code and their framework's lifecycle cleanup. For server rendering use [request-scoped clients](/ruxbase/integrations/server-side/) instead of this module singleton.

## Verification boundary
Shared SDK operations are covered by [Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js); React, Vue and Svelte framework builds, StrictMode behavior and browser session persistence are not verified here. See [schema and rules](/ruxbase/integrations/example-app/).
