import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 后端会静态托管 web/dist；dev 时把 /api 与 /healthz 代理到本地后端 (3000)。
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:3000', changeOrigin: true },
      '/healthz': { target: 'http://localhost:3000', changeOrigin: true },
    },
  },
});
