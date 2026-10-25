import { api } from '$lib/api/client';

export type Mode = 'light' | 'dark' | 'system';
export type Palette = 'auto' | 'morandi' | 'monet' | 'wada' | 'sage' | 'night';
export type Lang = 'zh' | 'en';

export const PALETTES: Exclude<Palette, 'auto'>[] = ['morandi', 'monet', 'wada', 'sage', 'night'];

/** Swatches for the palette picker (light, accent, accent-2). */
export const SWATCHES: Record<Exclude<Palette, 'auto'>, [string, string, string]> = {
	morandi: ['#efebe6', '#a8877f', '#8e9e8c'],
	monet: ['#eef2f3', '#7c9cb4', '#b7a6c9'],
	wada: ['#f3eee3', '#c46243', '#5b8a72'],
	sage: ['#f4f0e8', '#8a9a7b', '#c8b79e'],
	night: ['#161b26', '#e8c27a', '#7f94b8']
};

const KEY = 'bl.prefs';

interface Stored {
	mode?: Mode;
	palette?: Palette;
	lang?: Lang;
	showExif?: boolean;
	showGps?: boolean;
	volume?: number;
	/** warn when this page has pulled more than N MB from the server (0 = off) */
	pageWarnMB?: number;
	/** preferred quality for LX sources: 128k | 320k | flac | flac24bit */
	lxQuality?: string;
	/** when a direct stream fails / is blocked, relay it through the server */
	autoRelay?: boolean;
	/** playback speed for songs (radio always plays at 1x) */
	rate?: number;
	/** equaliser on/off, preset name and 10 band gains in dB */
	eqOn?: boolean;
	eqPreset?: string;
	eq?: number[];
	/** spectrum under the record */
	spectrum?: boolean;
}

/**
 * User preferences. Persisted to localStorage immediately and, when signed
 * in, synced to the server (PUT /api/me/prefs) so they follow the user.
 */
class Prefs {
	mode = $state<Mode>('system');
	palette = $state<Palette>('auto');
	lang = $state<Lang>('zh');
	showExif = $state(false);
	showGps = $state(false);
	volume = $state(0.8);
	pageWarnMB = $state(500);
	lxQuality = $state('320k');
	autoRelay = $state(true);
	rate = $state(1);
	eqOn = $state(false);
	eqPreset = $state('flat');
	eq = $state<number[]>([0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
	spectrum = $state(false);

	/** resolved values actually applied to <html> */
	dark = $state(false);

	private syncTimer: ReturnType<typeof setTimeout> | undefined;
	private serverSync = false;

	init() {
		try {
			const s: Stored = JSON.parse(localStorage.getItem(KEY) || '{}');
			this.assign(s);
			if (!s.lang) this.lang = navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en';
		} catch {
			/* ignore */
		}
		matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => this.apply());
		this.apply();
	}

	private assign(s: Stored) {
		if (s.mode) this.mode = s.mode;
		if (s.palette) this.palette = s.palette;
		if (s.lang) this.lang = s.lang;
		if (typeof s.showExif === 'boolean') this.showExif = s.showExif;
		if (typeof s.showGps === 'boolean') this.showGps = s.showGps;
		if (typeof s.volume === 'number') this.volume = s.volume;
		if (typeof s.pageWarnMB === 'number' && s.pageWarnMB >= 0) this.pageWarnMB = s.pageWarnMB;
		if (typeof s.lxQuality === 'string' && s.lxQuality) this.lxQuality = s.lxQuality;
		if (typeof s.autoRelay === 'boolean') this.autoRelay = s.autoRelay;
		if (typeof s.rate === 'number' && s.rate >= 0.5 && s.rate <= 2) this.rate = s.rate;
		if (typeof s.eqOn === 'boolean') this.eqOn = s.eqOn;
		if (typeof s.eqPreset === 'string') this.eqPreset = s.eqPreset;
		if (Array.isArray(s.eq) && s.eq.length === 10 && s.eq.every((g) => typeof g === 'number'))
			this.eq = s.eq.map((g) => Math.max(-12, Math.min(12, g)));
		if (typeof s.spectrum === 'boolean') this.spectrum = s.spectrum;
	}

	/** Load server-side prefs after login (server wins over local). */
	async loadServer() {
		this.serverSync = true;
		try {
			const s = await api<Stored>('/api/me/prefs');
			if (s && Object.keys(s).length) {
				this.assign(s);
				this.apply();
				this.persistLocal();
			} else {
				this.save(); // first login on this account: upload local prefs
			}
		} catch {
			/* offline: keep local */
		}
	}

	stopServerSync() {
		this.serverSync = false;
	}

	set<K extends keyof Stored>(key: K, value: NonNullable<Stored[K]>) {
		(this as unknown as Record<string, unknown>)[key] = value;
		this.apply();
		this.save();
	}

	apply() {
		const sysDark = matchMedia('(prefers-color-scheme: dark)').matches;
		this.dark = this.mode === 'dark' || (this.mode === 'system' && sysDark);
		const pal = this.palette === 'auto' ? (this.dark ? 'night' : 'morandi') : this.palette;
		const h = document.documentElement;
		h.dataset.mode = this.dark ? 'dark' : 'light';
		h.dataset.palette = pal;
		h.lang = this.lang === 'zh' ? 'zh-CN' : 'en';
		const meta = document.querySelector('meta[name="theme-color"]');
		if (meta) {
			// read the resolved --bg after the attribute change
			requestAnimationFrame(() =>
				meta.setAttribute('content', getComputedStyle(h).getPropertyValue('--bg').trim())
			);
		}
	}

	private snapshot(): Stored {
		return {
			mode: this.mode,
			palette: this.palette,
			lang: this.lang,
			showExif: this.showExif,
			showGps: this.showGps,
			volume: Math.round(this.volume * 100) / 100,
			pageWarnMB: this.pageWarnMB,
			lxQuality: this.lxQuality,
			autoRelay: this.autoRelay,
			rate: this.rate,
			eqOn: this.eqOn,
			eqPreset: this.eqPreset,
			eq: [...this.eq],
			spectrum: this.spectrum
		};
	}

	private persistLocal() {
		try {
			localStorage.setItem(KEY, JSON.stringify(this.snapshot()));
		} catch {
			/* private mode */
		}
	}

	save() {
		this.persistLocal();
		if (!this.serverSync) return;
		clearTimeout(this.syncTimer);
		this.syncTimer = setTimeout(() => {
			api('/api/me/prefs', { method: 'PUT', body: this.snapshot() }).catch(() => {});
		}, 800);
	}
}

export const prefs = new Prefs();
