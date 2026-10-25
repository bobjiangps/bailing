import { goto } from '$app/navigation';
import { base } from '$app/paths';
import { api, setUnauthorizedHandler, type Features, type User } from '$lib/api/client';
import { features } from '$lib/features.svelte';
import { collections } from './collections.svelte';
import { prefs } from './prefs.svelte';

class Session {
	user = $state<User | null>(null);
	noAuth = $state(false);
	ready = $state(false);

	async load() {
		try {
			const r = await api<{ user: User | null; no_auth: boolean; features?: Features }>('/api/auth/me');
			this.user = r.user;
			this.noAuth = r.no_auth;
			features.set(r.features);
		} catch {
			this.user = null;
		}
		this.ready = true;
		if (this.user) this.afterLogin();
	}

	private afterLogin() {
		prefs.loadServer();
		if (features.music) collections.load();
	}

	async login(name: string, password: string, remember: boolean) {
		const r = await api<{ user: User }>('/api/auth/login', { body: { name, password, remember } });
		this.user = r.user;
		this.afterLogin();
	}

	async logout() {
		await api('/api/auth/logout', { body: {} }).catch(() => {});
		this.user = null;
		prefs.stopServerSync();
		collections.reset();
		goto(base + '/');
	}
}

export const session = new Session();

setUnauthorizedHandler(() => {
	if (session.user && !session.noAuth) {
		session.user = null;
		const here = location.pathname + location.search;
		goto(`${base}/login?next=${encodeURIComponent(here)}`);
	}
});

/** Call from a page's $effect: redirects guests to the login page. */
export function requireLogin() {
	if (session.ready && !session.user) {
		const here = location.pathname + location.search;
		goto(`${base}/login?next=${encodeURIComponent(here)}`, { replaceState: true });
	}
}
