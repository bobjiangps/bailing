import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	// The Go API (make dev-api) listens on :8080; Vite forwards API/media calls
	// to it so the browser sees a single origin (cookies just work).
	// Override with the BAILING_API env var or a web/.env.local file.
	// loadEnv with prefix '' also reads the shell environment, so `process`
	// (and @types/node) is not needed here.
	const env = loadEnv(mode, '.', '');
	const api = env.BAILING_API || 'http://127.0.0.1:8080';

	return {
		plugins: [sveltekit()],
		server: {
			port: 5173,
			host: true, // reachable from your phone on the same Wi-Fi for touch testing
			proxy: {
				'/api': api,
				'/media': api,
				'/proxy': api
			}
		},
		build: {
			target: 'es2020',
			chunkSizeWarningLimit: 600
		}
	};
});
