import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: 'export',
  trailingSlash: true,
  images: {
    unoptimized: true,
  },
  // 安全审计 H-2：移除 typescript.ignoreBuildErrors。
  // 该开关原会把类型错误静默吞掉，让 XSS / 原型污染 / 越权类型混淆等
  // 隐患穿越编辑器到达运行时（例如 API 返回 unknown 但代码当 string 拼 DOM）。
  // 现在 CI 构建会因任何类型错误失败，强制开发者收敛。
  // 如某个文件确实需要绕过，应在该文件内 // @ts-expect-error 局部豁免。
};

export default nextConfig;
