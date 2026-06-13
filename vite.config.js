import { defineConfig } from 'vite';

export default defineConfig({
  root: '.',
  build: {
    outDir: 'static/dist',
    emptyOutDir: true,
    rollupOptions: {
      input: 'src/main.js',
      output: {
        entryFileNames: 'app.js',
        assetFileNames: 'app.[ext]',
      },
    },
  },
});
