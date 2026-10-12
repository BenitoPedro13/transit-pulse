import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* config options here */
  cacheComponents: true,
  partialPrefetching: true,
  // The read-only API (apps/api, `make api`) is reached same-origin, so there is no CORS.
  // Production Caddy does the same mapping, and this rewrite is not used there. API_ORIGIN points it
  // at another API (a second one on another port, say); the default is `make api`.
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${process.env.API_ORIGIN ?? "http://127.0.0.1:8080"}/:path*` }];
  },
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
