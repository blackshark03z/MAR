import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: '../../cmd/mar/owner_ui_dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    minify: false,
    sourcemap: false,
  },
})
