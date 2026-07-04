import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The Go daemon serves the built app from its own origin on port 7433.
// In dev we proxy /api (including the SSE stream) to the running daemon.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:7433",
        changeOrigin: true,
        // SSE needs a live, unbuffered connection.
        configure: (proxy) => {
          proxy.on("proxyReq", (proxyReq) => {
            proxyReq.setHeader("Accept-Encoding", "identity");
          });
        },
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
