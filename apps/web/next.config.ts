import type { NextConfig } from 'next';
import { config } from 'dotenv';
import { resolve } from 'path';
import {
  resolveDevDocsUrl,
  resolveDevRemoteApiUrl,
  resolveDocsUrl,
  resolveRemoteApiUrl,
} from './config/runtime-urls';

config({ path: resolve(__dirname, '../../.env') });

const isDev = process.env.NODE_ENV === 'development';
const remoteApiUrl = isDev ? resolveDevRemoteApiUrl(process.env) : resolveRemoteApiUrl(process.env);
const docsUrl = isDev ? resolveDevDocsUrl(process.env) : resolveDocsUrl(process.env);

const allowedDevOrigins = process.env.CORS_ALLOWED_ORIGINS
  ? process.env.CORS_ALLOWED_ORIGINS.split(',')
      .map((origin) => {
        try {
          return new URL(origin.trim()).host;
        } catch {
          return origin.trim();
        }
      })
      .filter(Boolean)
  : undefined;

const nextConfig: NextConfig = {
  ...(process.env.STANDALONE === 'true' ? { output: 'standalone' as const } : {}),
  transpilePackages: ['@goosar/core', '@goosar/ui', '@goosar/views'],
  ...(allowedDevOrigins && allowedDevOrigins.length > 0 ? { allowedDevOrigins } : {}),
  images: {
    formats: ['image/avif', 'image/webp'],
    qualities: [75, 80, 85],
  },
  async rewrites() {
    return {
      beforeFiles: docsUrl
        ? [
            {
              source: '/docs',
              destination: `${docsUrl}/docs`,
            },
            {
              source: '/docs/:path*',
              destination: `${docsUrl}/docs/:path*`,
            },
          ]
        : [],
      afterFiles: remoteApiUrl
        ? [
            {
              source: '/api/:path*',
              destination: `${remoteApiUrl}/api/:path*`,
            },
            {
              source: '/ws',
              destination: `${remoteApiUrl}/ws`,
            },
            {
              source: '/auth/:path*',
              destination: `${remoteApiUrl}/auth/:path*`,
            },
            {
              source: '/uploads/:path*',
              destination: `${remoteApiUrl}/uploads/:path*`,
            },
          ]
        : [],
      fallback: [],
    };
  },
};

export default nextConfig;
