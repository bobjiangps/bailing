/// <reference types="@sveltejs/kit" />
/// <reference no-default-lib="true"/>
/// <reference lib="esnext" />
/// <reference lib="webworker" />

// Service worker: makes the app installable (PWA) and opens instantly.
//
// Cached: the app shell only (JS/CSS chunks, fonts, icons, the HTML page).
// Never cached: /api, /media, /proxy. Photos, music and live data always
// come from the server, so the worker never serves stale content or eats
// the phone's storage.
import { build, files, version } from '$service-worker';

const sw = self as unknown as ServiceWorkerGlobalScope;
const CACHE = `bailing-${version}`;
const SHELL = 'shell'; // key of the cached SPA page
const ASSETS = [...build, ...files.filter((f) => !f.endsWith('.webmanifest'))];

sw.addEventListener('install', (e) => {
	e.waitUntil(caches.open(CACHE).then((c) => c.addAll(ASSETS)));
	// do not skipWaiting here: the page asks the user first (UpdateToast)
});

sw.addEventListener('activate', (e) => {
	e.waitUntil(
		(async () => {
			for (const k of await caches.keys()) if (k !== CACHE) await caches.delete(k);
			await sw.clients.claim();
		})()
	);
});

sw.addEventListener('message', (e) => {
	if (e.data === 'skipWaiting') sw.skipWaiting();
});

const NEVER = ['/api/', '/media/', '/proxy/'];

sw.addEventListener('fetch', (e) => {
	const req = e.request;
	if (req.method !== 'GET') return;
	const url = new URL(req.url);
	if (url.origin !== location.origin) return;
	const scope = new URL(sw.registration.scope).pathname; // "/" or "/bailing/"
	const path = url.pathname.slice(scope.length - 1);
	if (NEVER.some((p) => path.startsWith(p))) return;

	if (ASSETS.includes(url.pathname)) {
		e.respondWith(caches.match(req).then((r) => r ?? fetch(req)));
		return;
	}

	if (req.mode === 'navigate') {
		// network first, so a deployed update shows up; offline: last shell
		e.respondWith(
			(async () => {
				const cache = await caches.open(CACHE);
				try {
					const res = await fetch(req);
					if (res.ok) cache.put(SHELL, res.clone());
					return res;
				} catch {
					return (await cache.match(SHELL)) ?? Response.error();
				}
			})()
		);
	}
});

