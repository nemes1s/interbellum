import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The container image runs `node server.js` out of a minimal standalone
  // bundle rather than a full node_modules tree — see frontend/Dockerfile.
  output: "standalone",
  reactStrictMode: true,
  eslint: {
    // Lint is a separate command (`npm run lint`) and a separate CI job.
    // Running it inside `next build` would fail the container build on a
    // style nit, which is not what a build step is for.
    ignoreDuringBuilds: true,
  },
};

export default nextConfig;
