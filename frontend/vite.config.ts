import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	optimizeDeps: {
		// TinyH264 creates its worker with `new URL("./worker.js", import.meta.url)`.
		// Pre-bundling rewrites `import.meta.url` to `.vite/deps`, where the worker
		// file doesn't exist, so keep it unbundled in dev.
		exclude: ["@yume-chan/scrcpy-decoder-tinyh264"],
		// Its dependencies are CommonJS and still need pre-bundling.
		include: ["tinyh264", "yuv-canvas", "yuv-buffer"],
	},
	server: {
		host: "0.0.0.0",
		allowedHosts: true,
		proxy: {
			"/api": { target: "http://127.0.0.1:33299", ws: true, changeOrigin: true },
			"/mcp/": "http://127.0.0.1:33299",
			"/plugins/": "http://127.0.0.1:33299",
			"/stream": { target: "http://127.0.0.1:33299", ws: true, changeOrigin: true },
			"/proxy": "http://127.0.0.1:33299",
			"/plugin-runtime.d.ts": "http://127.0.0.1:33299",
			"/plugin.schema.json": "http://127.0.0.1:33299"
		}
	}
});
