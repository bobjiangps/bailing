import { api } from '$lib/api/client';
import { prefs } from '$lib/stores/prefs.svelte';

export type TrafficLevel = 'ok' | 'warn' | 'high';

interface PageInfo {
	bytes: number;
	requests: number;
	by_category: Record<string, number>;
	started_at?: number;
	active: boolean;
}

/**
 * Running total of everything the server has sent to THIS opened page
 * (music, relays, photos, covers, app files, API). The server keys it by
 * the bl_pg cookie that app.html renews on every page load, so the total
 * restarts at zero after a reload / reopen.
 *
 * Polled while something is going on: every 4s while audio plays through
 * the server, every 15s while the page is visible, not at all when hidden
 * and idle. One poll costs ~150 bytes, which is counted too.
 */
class PageTraffic {
	bytes = $state(0);
	requests = $state(0);
	byCat = $state<Record<string, number>>({});
	/** client time the page was opened (ms) */
	readonly openedAt = typeof performance !== 'undefined' ? Date.now() - performance.now() : Date.now();

	level = $derived.by<TrafficLevel>(() => {
		const lim = prefs.pageWarnMB * 1024 * 1024;
		if (lim <= 0) return 'ok';
		if (this.bytes >= lim * 2) return 'high';
		if (this.bytes >= lim) return 'warn';
		return 'ok';
	});

	private timer: ReturnType<typeof setTimeout> | undefined;
	private started = false;
	private listening = false;
	private busy: () => boolean = () => false;

	/** Start polling. busy() tells whether server audio is playing right now. */
	start(busy: () => boolean) {
		if (this.started || typeof window === 'undefined') return;
		this.started = true;
		this.busy = busy;
		if (!this.listening) {
			this.listening = true;
			document.addEventListener('visibilitychange', () => {
				if (document.visibilityState === 'visible') this.refresh();
			});
		}
		this.schedule(1200);
	}

	stop() {
		this.started = false;
		clearTimeout(this.timer);
	}

	/** Poll now (e.g. right after a track change) and reschedule. */
	async refresh() {
		if (!this.started) return; // signed out: never call the API (would 401)
		clearTimeout(this.timer);
		try {
			const r = await api<PageInfo>('/api/traffic/page');
			this.bytes = r.bytes;
			this.requests = r.requests;
			this.byCat = r.by_category ?? {};
		} catch {
			/* signed out / offline: keep the last value */
		}
		if (this.started) this.schedule();
	}

	private schedule(delay?: number) {
		clearTimeout(this.timer);
		const visible = document.visibilityState === 'visible';
		const playing = this.busy();
		if (delay === undefined) {
			if (playing) delay = 4000;
			else if (visible) delay = 15000;
			else delay = 60000; // hidden & idle: check rarely, just to notice playback resuming
		}
		this.timer = setTimeout(() => this.refresh(), delay);
	}
}

export const pageTraffic = new PageTraffic();




