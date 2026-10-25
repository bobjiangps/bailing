import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

// BASE_PATH=/bailing npm run build  -> app served under example.com/bailing/
// (must match server.base_path in config.yaml)
const base = process.env.BASE_PATH ?? '';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// Output goes straight into the Go embed directory.
		adapter: adapter({
			pages: '../internal/webui/dist',
			assets: '../internal/webui/dist',
			fallback: 'index.html', // SPA: every unknown path renders index.html
			precompress: false, // nginx compresses on the fly
			strict: false
		}),
		paths: { base, relative: false },
		// src/service-worker.ts is registered by $lib/pwa.svelte.ts (with an
		// update prompt), not automatically
		serviceWorker: { register: false }
	}
};

export default config;
