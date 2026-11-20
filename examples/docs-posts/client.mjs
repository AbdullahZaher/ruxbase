export async function requestJson(base, path, { method = "GET", body, token } = {}) {
    const headers = {};
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (token) headers.Authorization = token;
    const response = await fetch(new URL(path, base), {
        method, headers, body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!response.ok) throw new Error("Request failed: " + response.status);
    return response.status === 204 ? null : response.json();
}

export async function signIn(base, email, password) {
    const result = await requestJson(base, "/api/collections/users/auth-with-password", {
        method: "POST", body: { identity: email, password },
    });
    if (!result.token || !result.record?.id) throw new Error("Incomplete login");
    return { token: result.token, userId: result.record.id };
}

export async function createDraft(base, collection, session, title) {
    return requestJson(base, "/api/collections/" + encodeURIComponent(collection) + "/records", {
        method: "POST", token: session.token,
        body: { title, author: session.userId, published: false },
    });
}

export async function readPosts(base, collection, session) {
    return requestJson(base, "/api/collections/" + encodeURIComponent(collection) + "/records?page=1&perPage=50&sort=id", {
        token: session?.token,
    });
}
