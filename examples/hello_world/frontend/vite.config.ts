import { defineConfig } from "vite";

export default defineConfig({
    server: {
        port: 5173,
        strictPort: true,
        proxy: {
            "/intercall": {
                target: "http://127.0.0.1:8080",
                ws: true,
                // This local-only proxy must present a same-origin request to
                // the Go WebSocket handler. Do not use origin rewriting as an
                // authentication mechanism in production.
                rewriteWsOrigin: true,
            },
        },
    },
});
