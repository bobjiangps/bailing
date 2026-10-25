import {
	api,
	apiBase,
	ApiError,
	beacon,
	coverUrl,
	trackUrl,
	type LyricsResult,
	type ResolveResult,
	type SavedItem,
	type Track,
	type TrackRef
} from '$lib/api/client';
import { randomId } from '$lib/util/format';
import { prefs } from '$lib/stores/prefs.svelte';
import { pageTraffic } from '$lib/stores/pageTraffic.svelte';
import { fx } from './fx.svelte';

export const RATES = [0.75, 1, 1.25, 1.5, 2];
export const SLEEP_MINUTES = [15, 30, 45, 60, 90];
/** how long an upgraded https / direct attempt may stay silent before falling back */
const DIRECT_TIMEOUT_MS = 9000;
/** volume fade before the sleep timer pauses playback */
const SLEEP_FADE_MS = 8000;

export type PlayMode = 'order' | 'loop' | 'one' | 'shuffle';
export const MODES: PlayMode[] = ['loop', 'order', 'one', 'shuffle'];

/**
 * One queue entry. Local tracks are streamed by bailing ('server');
 * online items (direct URLs, radio, LX sources) are resolved when played
 * and go 'direct' (browser ↔ origin) or 'proxy' (relayed by the server).
 */
export interface QueueItem extends Track {
	source: string; // 'local' | 'direct' | 'radio' | 'lx:kw' ...
	url?: string;
	/** server = streamed by bailing; proxy = relayed; direct = browser ↔ origin */
	traffic: 'server' | 'proxy' | 'direct';
	/** source-private data (radio stream info, LX musicInfo) */
	extra?: Record<string, unknown>;
	/** live stream (radio): no duration, no seeking */
	live?: boolean;
}

export const fromTrack = (t: Track): QueueItem => ({ ...t, source: 'local', traffic: 'server' });

/** A favourite / playlist entry back into a queue item. */
export const fromSaved = (s: SavedItem): QueueItem =>
	s.source === 'local'
		? fromTrack({
				id: s.id,
				title: s.title,
				artist: s.artist,
				album: s.album,
				duration_ms: s.duration_ms,
				cover: s.cover,
				color: s.color || '',
				format: s.format || '',
				folder: s.folder || ''
			})
		: fromRef(s);

export const fromRef = (r: TrackRef): QueueItem => ({
	id: r.id,
	title: r.title || r.id,
	artist: r.artist || '',
	album: r.album || '',
	duration_ms: r.duration_ms || 0,
	cover: r.cover || '',
	color: '',
	format: '',
	folder: '',
	source: r.source,
	traffic: 'direct',
	extra: r.extra,
	live: r.source === 'radio'
});

export const toRef = (q: QueueItem): TrackRef => ({
	source: q.source,
	id: q.id,
	title: q.title,
	artist: q.artist,
	album: q.album,
	duration_ms: q.duration_ms,
	cover: q.cover,
	extra: q.extra
});

// Online items played recently, so "recently played" on the home page can
// play them again (the server only stores source + id + title).
const ONLINE_KEY = 'bl.online';
function rememberOnline(q: QueueItem) {
	try {
		const m: Record<string, TrackRef> = JSON.parse(localStorage.getItem(ONLINE_KEY) || '{}');
		const k = q.source + '|' + q.id;
		delete m[k];
		m[k] = toRef(q);
		const keys = Object.keys(m);
		for (const old of keys.slice(0, Math.max(0, keys.length - 300))) delete m[old];
		localStorage.setItem(ONLINE_KEY, JSON.stringify(m));
	} catch {
		/* quota */
	}
}
export function recallOnline(source: string, id: string): TrackRef | null {
	try {
		const m: Record<string, TrackRef> = JSON.parse(localStorage.getItem(ONLINE_KEY) || '{}');
		return m[source + '|' + id] ?? null;
	} catch {
		return null;
	}
}

const STORE_KEY = 'bl.player';

/**
 * The single audio engine of the app. The <audio> element lives in the root
 * layout, so navigation never interrupts playback.
 */
class Player {
	queue = $state<QueueItem[]>([]);
	index = $state(-1);
	playing = $state(false);
	loading = $state(false);
	error = $state('');
	/** server message behind an error (e.g. why a source could not resolve) */
	errorDetail = $state('');
	/** i18n key of a transient notice (e.g. "switched to relay") */
	notice = $state('');
	/** current item is a live stream (radio) */
	live = $state(false);
	time = $state(0);
	duration = $state(0);
	muted = $state(false);
	mode = $state<PlayMode>('loop');

	/** sleep timer: epoch ms when playback stops (0 = off) */
	sleepAt = $state(0);
	/** stop when the current song ends */
	sleepAfterTrack = $state(false);
	/** seconds left on the sleep timer (refreshed every second) */
	sleepLeft = $state(0);

	lyrics = $state<LyricsResult | null>(null);
	lyricsLoading = $state(false);

	/** traffic of the current server stream */
	sid = $state('');
	streamBytes = $state(0);
	listened = $state(0); // seconds actually played in this session
	/** seconds played through the server (server + proxy) since this page was opened */
	serverListened = $state(0);

	current = $derived(this.index >= 0 ? (this.queue[this.index] ?? null) : null);

	private audio: HTMLAudioElement | null = null;
	private history: number[] = [];
	private counted = false;
	private lastTick = 0;
	private restoreTime = 0;
	private pollTimer: ReturnType<typeof setInterval> | undefined;
	private persistTimer: ReturnType<typeof setInterval> | undefined;
	private hls: { destroy(): void } | null = null;
	private loadToken = 0;
	private fallbackUrl = '';
	private lastFormat = '';
	private sleepTimer: ReturnType<typeof setInterval> | undefined;
	private fading = false;
	private directTimer: ReturnType<typeof setTimeout> | undefined;

	attach(el: HTMLAudioElement) {
		if (this.audio === el) return;
		this.audio = el;
		el.volume = prefs.volume;
		el.preservesPitch = true;
		if (prefs.eqOn || prefs.spectrum) this.enableFx(false);
		el.addEventListener('play', () => {
			this.playing = true;
			this.lastTick = performance.now();
			setTimeout(() => pageTraffic.refresh(), 1000); // switch to fast polling
		});
		el.addEventListener('pause', () => (this.playing = false));
		el.addEventListener('waiting', () => (this.loading = true));
		el.addEventListener('playing', () => {
			this.loading = false;
			this.error = '';
			clearTimeout(this.directTimer);
			if (this.notice === 'player.httpsTry') this.notice = '';
		});
		el.addEventListener('canplay', () => (this.loading = false));
		el.addEventListener('loadedmetadata', () => {
			this.duration = isFinite(el.duration) ? el.duration : 0;
			if (!isFinite(el.duration)) this.live = true;
			if (this.restoreTime > 0 && this.restoreTime < this.duration) {
				el.currentTime = this.restoreTime;
			}
			this.restoreTime = 0;
			this.applyRate();
			const cur = this.current;
			if (cur && cur.source === 'local' && !cur.duration_ms && this.duration > 0) {
				cur.duration_ms = Math.round(this.duration * 1000);
				api(`/api/music/tracks/${cur.id}/duration`, { body: { ms: cur.duration_ms } }).catch(() => {});
			}
		});
		el.addEventListener('timeupdate', () => this.onTime());
		el.addEventListener('ended', () => {
			if (this.sleepAfterTrack) {
				this.sleepAfterTrack = false; // stop here: do not advance
				return;
			}
			this.next(true);
		});
		el.addEventListener('error', () => {
			if (!el.src) return;
			this.onPlaybackError();
		});
		el.addEventListener('volumechange', () => (this.muted = el.muted));
		this.setupMediaSession();
		this.restore();
		this.persistTimer = setInterval(() => this.persist(), 5000);
		window.addEventListener('pagehide', () => this.persist());
	}

	// ---- queue control ------------------------------------------------------------

	playList(items: QueueItem[], start = 0) {
		this.queue = items;
		this.history = [];
		this.playAt(start);
	}

	playAt(i: number, autoplay = true) {
		if (i < 0 || i >= this.queue.length || !this.audio) return;
		if (this.index >= 0 && this.index !== i) this.history.push(this.index);
		if (this.history.length > 200) this.history.shift();
		this.index = i;
		this.load(autoplay);
	}

	/** Insert after the current item ("play next"); plays now if idle. */
	playNext(item: QueueItem) {
		if (!this.current) {
			this.playList([item], 0);
			return;
		}
		const q = [...this.queue];
		q.splice(this.index + 1, 0, item);
		this.queue = q;
	}

	remove(i: number) {
		if (i === this.index) {
			const q = this.queue.filter((_, k) => k !== i);
			this.queue = q;
			if (!q.length) return this.clear();
			this.index = Math.min(i, q.length - 1);
			this.history = [];
			this.load(this.playing);
			return;
		}
		this.queue = this.queue.filter((_, k) => k !== i);
		if (i < this.index) this.index--;
		this.history = this.history.filter((h) => h !== i).map((h) => (h > i ? h - 1 : h));
	}

	clear() {
		this.loadToken++;
		this.destroyHls();
		this.audio?.pause();
		this.audio?.removeAttribute('src');
		this.audio?.load();
		this.queue = [];
		this.index = -1;
		this.history = [];
		this.time = this.duration = 0;
		this.lyrics = null;
		this.error = this.notice = '';
		this.live = false;
		this.stopPolling();
		this.persist();
	}

	toggle() {
		const a = this.audio;
		if (!a || !this.current) return;
		if (a.paused) {
			// not loaded yet (online items resolve lazily) or failed: (re)load
			if (!a.src || this.error) this.load(true);
			else a.play().catch(() => {});
		} else a.pause();
	}

	next(auto = false) {
		const n = this.queue.length;
		if (!n) return;
		if (auto && this.mode === 'one') {
			this.audio!.currentTime = 0;
			this.newSession();
			this.audio!.play().catch(() => {});
			return;
		}
		let i: number;
		if (this.mode === 'shuffle' && n > 1) {
			do i = Math.floor(Math.random() * n);
			while (i === this.index);
		} else {
			i = this.index + 1;
			if (i >= n) {
				if (auto && this.mode === 'order') {
					this.audio?.pause();
					return;
				}
				i = 0;
			}
		}
		this.playAt(i);
	}

	prev() {
		const a = this.audio;
		if (a && a.currentTime > 4) {
			a.currentTime = 0;
			return;
		}
		if (this.mode === 'shuffle' && this.history.length) {
			const i = this.history.pop()!;
			this.index = i;
			this.load(true);
			return;
		}
		const n = this.queue.length;
		if (!n) return;
		this.playAt((this.index - 1 + n) % n);
	}

	seek(sec: number) {
		if (this.audio && isFinite(sec)) this.audio.currentTime = Math.max(0, sec);
	}

	setVolume(v: number) {
		v = Math.min(1, Math.max(0, v));
		if (this.audio) {
			this.audio.volume = v;
			if (v > 0) this.audio.muted = false;
		}
		prefs.set('volume', v);
	}

	toggleMute() {
		if (this.audio) this.audio.muted = !this.audio.muted;
	}

	cycleMode() {
		this.mode = MODES[(MODES.indexOf(this.mode) + 1) % MODES.length];
		this.persist();
	}

	// ---- internals ----------------------------------------------------------------

	private newSession() {
		this.sid = randomId(16);
		this.streamBytes = 0;
		this.listened = 0;
		this.counted = false;
	}

	private load(autoplay: boolean) {
		const cur = this.current;
		const a = this.audio;
		if (!cur || !a) return;
		const token = ++this.loadToken;
		this.newSession();
		this.error = this.errorDetail = this.notice = '';
		this.time = 0;
		this.duration = cur.duration_ms / 1000;
		this.live = !!cur.live;
		this.fallbackUrl = '';
		this.destroyHls();
		this.loadLyrics(cur);
		this.updateMediaSession(cur);
		this.persist();

		if (cur.source === 'local') {
			cur.traffic = 'server';
			this.loading = true;
			this.setCors(a, 'server');
			a.src = trackUrl(cur.id, this.sid);
			if (autoplay) a.play().catch(() => (this.loading = false));
			else this.loading = false;
			this.startPolling();
			return;
		}

		// online: resolved only when actually played (URLs expire, radio
		// counts clicks), so a restored queue costs nothing on page load
		this.stopPolling();
		a.removeAttribute('src');
		a.load();
		if (!autoplay) {
			this.loading = false;
			return;
		}
		this.loading = true;
		rememberOnline(cur);
		this.resolveAndPlay(cur, token);
	}

	private async resolveAndPlay(cur: QueueItem, token: number) {
		let r: ResolveResult;
		try {
			r = await api<ResolveResult>('/api/play/resolve', {
				body: { source: cur.source, track: toRef(cur), quality: prefs.lxQuality, need_cors: fx.attached }
			});
		} catch (e) {
			if (token !== this.loadToken) return;
			const msg = e instanceof ApiError ? e.message : '';
			// the script answered with a link its own (dead) backend cannot serve
			this.fail(msg.includes('dead link') ? 'player.deadLink' : 'player.resolveError', msg);
			return;
		}
		if (token !== this.loadToken || !this.audio) return;

		const proxyUrl = r.proxy_url ? `${apiBase}${r.proxy_url}&sid=${this.sid}` : '';
		// An https page cannot play http audio (mixed content). Many stations
		// serve the same stream over https too, so try that first and only
		// fall back to the (traffic-costing) relay when it does not answer.
		const mixed = location.protocol === 'https:' && r.url.startsWith('http:');
		const directUrl = mixed ? 'https:' + r.url.slice(5) : r.url;
		const nativeHls = !!this.audio.canPlayType('application/vnd.apple.mpegurl');
		// with the equaliser / spectrum attached, cross-origin audio must allow CORS
		const fxBlocked = fx.attached && !r.cors;
		const needRelay =
			r.mode === 'proxy' || (r.format === 'hls' && !r.cors && !nativeHls) || (fxBlocked && !mixed);
		this.lastFormat = r.format;
		if (needRelay) {
			if (!proxyUrl) return this.fail('player.relayDisabled');
			if (!prefs.autoRelay) return this.fail(fxBlocked ? 'player.fxNeedsRelay' : 'player.relayOff');
			this.notice = fxBlocked && r.mode !== 'proxy' ? 'player.relayFx' : 'player.relayNeeded';
			this.playUrl(proxyUrl, r.format, 'proxy', token);
		} else {
			this.fallbackUrl = proxyUrl;
			this.notice = mixed ? 'player.httpsTry' : '';
			this.playUrl(directUrl, r.format, 'direct', token);
			// a stream that neither plays nor fails (e.g. https on an http-only
			// port) would hang: give it a few seconds, then use the fallback
			if (proxyUrl) {
				clearTimeout(this.directTimer);
				this.directTimer = setTimeout(() => {
					const a = this.audio;
					if (token === this.loadToken && this.loading && a && a.readyState < 3) this.onPlaybackError();
				}, DIRECT_TIMEOUT_MS);
			}
		}
	}

	private async playUrl(url: string, format: string, via: 'direct' | 'proxy', token: number) {
		const a = this.audio;
		const cur = this.current;
		if (!a || !cur) return;
		cur.traffic = via;
		this.destroyHls();
		if (via === 'proxy') this.startPolling();
		else this.stopPolling();

		if (format === 'hls' && !a.canPlayType('application/vnd.apple.mpegurl')) {
			try {
				const { default: Hls } = await import('hls.js');
				if (token !== this.loadToken) return;
				if (!Hls.isSupported()) throw new Error('MSE unsupported');
				const hls = new Hls({ enableWorker: true, liveDurationInfinity: true });
				this.hls = hls;
				hls.on(Hls.Events.ERROR, (_e, data) => {
					if (data.fatal && token === this.loadToken) this.onPlaybackError();
				});
				hls.loadSource(url);
				hls.attachMedia(a);
			} catch {
				return this.fail('player.hlsError');
			}
		} else {
			this.setCors(a, via === 'direct' ? 'direct' : 'server');
			a.src = url;
		}
		this.applyRate();
		a.play().catch(() => (this.loading = false));
	}

	/**
	 * crossorigin attribute: only set while the Web Audio graph is attached
	 * (otherwise it would make origins without CORS fail for no reason).
	 */
	private setCors(a: HTMLAudioElement, kind: 'server' | 'direct') {
		if (!fx.attached) a.removeAttribute('crossorigin');
		else a.crossOrigin = kind === 'server' ? 'use-credentials' : 'anonymous';
	}

	// ---- speed / sleep timer / effects ----------------------------------------------

	/** Playback speed for songs; live radio always plays at 1x. */
	setRate(r: number) {
		prefs.set('rate', Math.min(2, Math.max(0.5, r)));
		this.applyRate();
	}

	private applyRate() {
		const a = this.audio;
		if (!a) return;
		const r = this.live ? 1 : prefs.rate;
		a.defaultPlaybackRate = r;
		if (a.playbackRate !== r) a.playbackRate = r;
	}

	/** minutes > 0: stop after that long; 'track': after this song; 0: off. */
	setSleep(v: number | 'track') {
		clearInterval(this.sleepTimer);
		this.sleepTimer = undefined;
		if (this.fading) this.endFade(false);
		this.sleepAfterTrack = v === 'track';
		this.sleepAt = typeof v === 'number' && v > 0 ? Date.now() + v * 60_000 : 0;
		this.sleepLeft = 0;
		if (!this.sleepAt) return;
		const tick = () => {
			const left = Math.max(0, Math.round((this.sleepAt - Date.now()) / 1000));
			this.sleepLeft = left;
			if (left * 1000 <= SLEEP_FADE_MS && !this.fading && this.playing) this.startFade();
			if (left <= 0) {
				clearInterval(this.sleepTimer);
				this.sleepTimer = undefined;
				this.sleepAt = 0;
				this.endFade(true);
			}
		};
		tick();
		this.sleepTimer = setInterval(tick, 1000);
	}

	/** fade animation frame */
	private fadeRaf = 0;

	private startFade() {
		const a = this.audio;
		if (!a) return;
		this.fading = true;
		const from = prefs.volume;
		const t0 = performance.now();
		const step = () => {
			if (!this.fading) return;
			const k = Math.min(1, (performance.now() - t0) / SLEEP_FADE_MS);
			a.volume = from * (1 - k);
			if (k < 1) this.fadeRaf = requestAnimationFrame(step);
		};
		this.fadeRaf = requestAnimationFrame(step);
	}

	/** Stop fading; pause when the timer ran out; restore the volume. */
	private endFade(pause: boolean) {
		cancelAnimationFrame(this.fadeRaf);
		this.fading = false;
		if (pause) this.audio?.pause();
		if (this.audio) this.audio.volume = prefs.volume;
	}

	/**
	 * Attach the Web Audio graph (equaliser / spectrum). Cannot be undone
	 * until reload; the current online stream is reloaded so it plays with
	 * the CORS mode the graph requires.
	 */
	enableFx(reload = true): boolean {
		const a = this.audio;
		if (!a) return false;
		if (fx.attached) return true;
		if (!fx.attach(a)) return false;
		fx.setEq(prefs.eqOn, prefs.eq);
		const cur = this.current;
		if (reload && cur && a.src) {
			const wasPlaying = !a.paused;
			if (!this.live) this.restoreTime = a.currentTime;
			this.load(wasPlaying);
		}
		return true;
	}

	/** A stream failed: retry once through the relay, otherwise show an error. */
	private onPlaybackError() {
		clearTimeout(this.directTimer);
		if (this.notice === 'player.httpsTry') this.notice = '';
		const cur = this.current;
		if (cur && cur.source !== 'local' && cur.traffic === 'direct' && this.fallbackUrl) {
			const u = this.fallbackUrl;
			this.fallbackUrl = '';
			if (prefs.autoRelay) {
				this.notice = 'player.relayFallback';
				this.loading = true;
				this.playUrl(u, this.lastFormat, 'proxy', this.loadToken);
				return;
			}
			return this.fail('player.directFailed');
		}
		this.fail('player.error');
	}

	private fail(key: string, detail = '') {
		this.loading = false;
		this.playing = false;
		this.error = key;
		this.errorDetail = detail;
		this.stopPolling();
	}

	private destroyHls() {
		if (this.hls) {
			try {
				this.hls.destroy();
			} catch {
				/* ignore */
			}
			this.hls = null;
		}
	}

	private async loadLyrics(cur: QueueItem) {
		this.lyrics = null;
		if (cur.live || cur.source === 'radio') {
			this.lyrics = { status: 'none', source: 'radio', synced: false, lines: [] };
			return;
		}
		this.lyricsLoading = true;
		try {
			let r: LyricsResult;
			if (cur.source === 'local') {
				r = await api<LyricsResult>(`/api/music/tracks/${cur.id}/lyrics`);
			} else {
				const p = new URLSearchParams({
					title: cur.title,
					artist: cur.artist,
					album: cur.album,
					duration_ms: String(cur.duration_ms || 0)
				});
				r = await api<LyricsResult>(`/api/lyrics/lookup?${p}`);
			}
			if (this.current === cur) this.lyrics = r;
		} catch {
			if (this.current === cur) this.lyrics = { status: 'none', source: '', synced: false, lines: [] };
		} finally {
			this.lyricsLoading = false;
		}
	}

	async refetchLyrics() {
		const cur = this.current;
		if (!cur || cur.source !== 'local') return;
		await api(`/api/music/tracks/${cur.id}/lyrics`, { method: 'DELETE' }).catch(() => {});
		this.loadLyrics(cur);
	}

	private onTime() {
		const a = this.audio!;
		this.time = a.currentTime;
		const now = performance.now();
		if (!a.paused && this.lastTick) {
			const d = (now - this.lastTick) / 1000;
			if (d > 0 && d < 2) {
				this.listened += d;
				if (this.current && this.current.traffic !== 'direct') this.serverListened += d;
			}
		}
		this.lastTick = now;
		// count a play at >= 30s or >= 50%
		const cur = this.current;
		if (!this.counted && cur && (this.listened >= 30 || (this.duration > 0 && this.listened >= this.duration / 2))) {
			this.counted = true;
			beacon('/api/stats/play', {
				source: cur.source,
				ref: cur.id,
				title: cur.title,
				artist: cur.artist,
				cover: cur.cover,
				listened_ms: Math.round(this.listened * 1000)
			});
			window.dispatchEvent(new Event('bl:stats')); // home notes refresh
		}
		if ('mediaSession' in navigator && this.duration > 0 && isFinite(a.currentTime)) {
			try {
				navigator.mediaSession.setPositionState({
					duration: this.duration,
					position: Math.min(a.currentTime, this.duration),
					playbackRate: a.playbackRate
				});
			} catch {
				/* ignore */
			}
		}
	}

	/** Poll the server's live byte counter for the current stream. */
	private startPolling() {
		this.stopPolling();
		const cur = this.current;
		if (!cur || cur.traffic === 'direct') return;
		const poll = async () => {
			if (!this.sid) return;
			try {
				const r = await api<{ bytes: number }>(`/api/traffic/streams/${this.sid}`);
				this.streamBytes = r.bytes;
			} catch {
				/* ignore */
			}
		};
		this.pollTimer = setInterval(() => {
			if (this.playing || this.loading) poll();
		}, 4000);
		setTimeout(poll, 1500);
		setTimeout(() => pageTraffic.refresh(), 1500);
	}

	private stopPolling() {
		clearInterval(this.pollTimer);
	}

	private setupMediaSession() {
		if (!('mediaSession' in navigator)) return;
		const ms = navigator.mediaSession;
		const set = (a: MediaSessionAction, h: MediaSessionActionHandler) => {
			try {
				ms.setActionHandler(a, h);
			} catch {
				/* unsupported action */
			}
		};
		set('play', () => this.toggle());
		set('pause', () => this.toggle());
		set('previoustrack', () => this.prev());
		set('nexttrack', () => this.next());
		set('seekbackward', () => this.seek(this.time - 10));
		set('seekforward', () => this.seek(this.time + 10));
		set('seekto', (d) => d.seekTime != null && this.seek(d.seekTime));
	}

	private updateMediaSession(cur: QueueItem) {
		if (!('mediaSession' in navigator)) return;
		const art = cur.cover
			? [{ src: new URL(coverUrl(cur.cover, 'm'), location.href).href, sizes: '800x800', type: 'image/webp' }]
			: [];
		navigator.mediaSession.metadata = new MediaMetadata({
			title: cur.title,
			artist: cur.artist,
			album: cur.album,
			artwork: art
		});
	}

	// ---- persistence ------------------------------------------------------------------

	private persist() {
		try {
			localStorage.setItem(
				STORE_KEY,
				JSON.stringify({
					queue: this.queue.slice(0, 1000),
					index: this.index,
					time: this.audio?.currentTime ?? this.time,
					mode: this.mode
				})
			);
		} catch {
			/* quota */
		}
	}

	private restore() {
		try {
			const s = JSON.parse(localStorage.getItem(STORE_KEY) || '{}');
			if (s.mode && MODES.includes(s.mode)) this.mode = s.mode;
			if (Array.isArray(s.queue) && s.queue.length && s.index >= 0 && s.index < s.queue.length) {
				this.queue = s.queue;
				this.index = s.index;
				this.restoreTime = s.time || 0;
				this.load(false); // ready to continue, but never autoplay on page load
			}
		} catch {
			/* ignore */
		}
	}
}

export const player = new Player();
