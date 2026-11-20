import PocketBase from "pocketbase";

export async function loadSession(baseUrl, cookieHeader) {
    const pb = new PocketBase(baseUrl);
    pb.authStore.loadFromCookie(cookieHeader ?? "");
    let user = null;
    if (pb.authStore.isValid) {
        try {
            const result = await pb.collection("users").authRefresh();
            user = result.record;
        } catch {
            pb.authStore.clear();
        }
    } else {
        pb.authStore.clear();
    }
    const setCookie = pb.authStore.exportToCookie({
        httpOnly: true,
        secure: true,
        sameSite: "lax",
        path: "/",
    });
    return { pb, user, setCookie };
}
