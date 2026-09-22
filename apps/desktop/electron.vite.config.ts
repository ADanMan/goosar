import { resolve } from 'path';
import { defineConfig, externalizeDepsPlugin } from 'electron-vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
    define: {
      __GOOSAR_DESKTOP_NO_UPDATER__: JSON.stringify(
        process.env.GOOSAR_DESKTOP_NO_UPDATER === '1' ||
          process.env.GOOSAR_DESKTOP_NO_UPDATER === 'true',
      ),
    },
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
  },
  renderer: {
    server: {
      port: Number(process.env.DESKTOP_RENDERER_PORT) || 5172,
      strictPort: true,
    },
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        '@': resolve('src/renderer/src'),
      },
      dedupe: ['react', 'react-dom', '@tanstack/react-query'],
    },
  },
});
