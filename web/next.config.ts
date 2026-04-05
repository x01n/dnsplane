import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: 'export',
  trailingSlash: true,
  images: {
    unoptimized: true,
  },
  // 禁用 TypeScript 错误导致构建失败
  typescript: {
    ignoreBuildErrors: true,
  },
};

export default nextConfig;
