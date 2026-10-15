import { defineConfig, type Plugin } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

type Target = 'wails' | 'mock';

// One source tree, one shipped build: `--mode wails` is the desktop
// application's user interface (dist/wails). `--mode mock` serves the same
// interface over the in-memory mock Service in a browser, for development
// before and besides the engine; it is never shipped.
function entryHtml(target: Target): Plugin {
  const entry = `index.${target}.html`;
  return {
    name: 'explorer-entry-html',
    enforce: 'post',
    configureServer(server) {
      server.middlewares.use((req, _res, next) => {
        const [path, query] = (req.url ?? '/').split('?');
        if (path === '/' || path === '/index.html') {
          req.url = `/${entry}${query === undefined ? '' : `?${query}`}`;
        }
        next();
      });
    },
    generateBundle(_options, bundle) {
      const html = bundle[entry];
      if (html) {
        html.fileName = 'index.html';
      }
    },
  };
}

export default defineConfig(({ mode }) => {
  const target: Target = mode === 'mock' ? 'mock' : 'wails';
  return {
    plugins: [react(), tailwindcss(), entryHtml(target)],
    build: {
      outDir: 'dist/wails',
      emptyOutDir: true,
      // The window runs under CSP `default-src 'self'`: a data: URI for a
      // small font or image would be blocked, so nothing is inlined.
      assetsInlineLimit: 0,
      rollupOptions: { input: 'index.wails.html' },
    },
    test: {
      environment: 'jsdom',
      setupFiles: ['src/test/setup.ts'],
      css: false,
    },
  };
});
