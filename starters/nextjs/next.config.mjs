const backend = process.env.RUXBASE_INTERNAL_URL || "http://127.0.0.1:8090";
const endpoint = new URL(backend);
if (!["http:", "https:"].includes(endpoint.protocol) || !endpoint.hostname ||
    endpoint.username || endpoint.password || endpoint.search || endpoint.hash ||
    endpoint.pathname !== "/" || endpoint.origin !== backend.replace(/\/$/, "")) {
  throw new Error("RUXBASE_INTERNAL_URL must be a trusted server origin without credentials or paths");
}
export default {
  // The browser uses only same-origin /api, never a secret or database URL.
  async rewrites() {
    return [{ source: "/api/:path*", destination: backend + "/api/:path*" }];
  },
};
