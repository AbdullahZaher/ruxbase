import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  server: { host: "0.0.0.0", allowedHosts: ["localhost", "127.0.0.1"], proxy: { "/api": { target: process.env.RUXBASE_INTERNAL_URL || "http://127.0.0.1:8090", changeOrigin: true } } },
  preview: { host: "0.0.0.0", proxy: { "/api": { target: process.env.RUXBASE_INTERNAL_URL || "http://127.0.0.1:8090", changeOrigin: true } } },
});
