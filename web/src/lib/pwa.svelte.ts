import { base } from '$app/paths';
import { dev } from '$app/environment';

/** A newer service worker is installed and waiting (shown by UpdateToast). */
export const pwa = $state({ waiting: null as ServiceWorker | null });

/**
 * Register the service worker (src/service-worker.ts). Skipped in dev and
 * inside the desktop shell, which loads the app from a local server anyway.
 */
export function registerSW() {
	if (dev || !('serviceWorker' in navigator) || window.__BAILING_DESKTOP__) return;
	navigator.serviceWorker
		.register(`${base}/service-worker.js`, { scope: `${base}/` })
		.then((reg) => {
			const track = (w: ServiceWorker | null) => {
				if (!w) return;
				w.addEventListener('statechange', () => {
					if (w.state === 'installed' && navigator.serviceWorker.controller) pwa.waiting = w;
				});
			};
			if (reg.waiting && navigator.serviceWorker.controller) pwa.waiting = reg.waiting;
			reg.addEventListener('updatefound', () => track(reg.installing));
			// check for a new version when the app comes back to the foreground
			document.addEventListener('visibilitychange', () => {
				if (document.visibilityState === 'visible') reg.update().catch(() => {});
			});
		})
		.catch(() => {});
	let reloaded = false;
	navigator.serviceWorker.addEventListener('controllerchange', () => {
		if (reloaded) return;
		reloaded = true;
		location.reload();
	});
}

export function applyUpdate() {
	pwa.waiting?.postMessage('skipWaiting');
}

