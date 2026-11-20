const backend = process.env.RUXBASE_INTERNAL_URL || "http://127.0.0.1:8090";
if (!/^https?:\/\/[^/?#]+$/.test(backend)) throw new Error("RUXBASE_INTERNAL_URL must be a trusted server origin");
export default {
  // The browser uses only same-origin /api, never a secret or database URL.
  async rewrites() {
    return [{ source: "/api/:path*", destination: backend + "/api/:path*" }];
  },
};
