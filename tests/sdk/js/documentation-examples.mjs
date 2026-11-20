export async function listPosts(client, collectionName, searchTerm, minimumViews) {
    return client.collection(collectionName).getList(1, 2, {
        filter: client.filter("title ~ {:q} && views >= {:min}", {
            q: searchTerm,
            min: minimumViews,
        }),
        sort: "views",
    });
}

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
