import type { NextConfig } from "next";
import path from "node:path";

const config: NextConfig = {
  poweredByHeader: false,
  devIndicators: false,
  distDir: process.env.NEMI_WEB_DIST_DIR || ".next",
  outputFileTracingRoot: path.resolve(process.cwd(), "../.."),
  async rewrites() {
    return [
      {
        source: "/api/v1/:path*",
        destination: `${process.env.API_ORIGIN || "http://127.0.0.1:8080"}/api/v1/:path*`,
      },
    ];
  },
};
export default config;
