import { base } from '$app/paths';

/** API origin + base path. Same-origin by default; the desktop shell may inject another. */
export const apiBase: string =
	(typeof window !== 'undefined' && window.__BAILING_API__) || base;

export class ApiError extends Error {
	status: number;
	code: string;
	constructor(status: number, code: string, message: string) {
		super(message);
		this.status = status;
		this.code = code;
	}
}

type Opts = { method?: string; body?: unknown; signal?: AbortSignal };

let onUnauthorized: (() => void) | null = null;
/** Registered by the session store: called when any request returns 401. */
export function setUnauthorizedHandler(fn: () => void) {
	onUnauthorized = fn;
}

export async function api<T = unknown>(path: string, opts: Opts = {}): Promise<T> {
	const init: RequestInit = {
		method: opts.method ?? (opts.body !== undefined ? 'POST' : 'GET'),
		credentials: 'include',
		signal: opts.signal,
		headers: opts.body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
		body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined
	};
	const res = await fetch(apiBase + path, init);
	if (!res.ok) {
		let code = 'error';
		let msg = res.statusText;
		try {
			const j = await res.json();
			code = j.code ?? code;
			msg = j.error ?? msg;
		} catch {
			/* not JSON */
		}
		if (res.status === 401 && onUnauthorized && !path.startsWith('/api/auth/')) onUnauthorized();
		throw new ApiError(res.status, code, msg);
	}
	return (await res.json()) as T;
}

/** Fire-and-forget POST that survives page unload (stats). */
export function beacon(path: string, body: unknown) {
	const data = JSON.stringify(body);
	if (navigator.sendBeacon) {
		const ok = navigator.sendBeacon(apiBase + path, new Blob([data], { type: 'application/json' }));
		if (ok) return;
	}
	fetch(apiBase + path, {
		method: 'POST',
		credentials: 'include',
		keepalive: true,
		headers: { 'Content-Type': 'application/json' },
		body: data
	}).catch(() => {});
}

// ---- media URLs -------------------------------------------------------------
export const photoUrl = (id: string, size: 's' | 'm' | 'o') => `${apiBase}/media/photo/${id}/${size}`;
/** Local covers are keys; online covers are full https URLs (loaded directly). */
export const coverUrl = (key: string, size: 's' | 'm' = 's') =>
	!key ? '' : /^https?:\/\//.test(key) ? key : `${apiBase}/media/cover/${key}?size=${size}`;
export const trackUrl = (id: string, sid: string) => `${apiBase}/media/track/${id}?sid=${sid}`;

/**
 * Svelte action for <img>: an online cover that the browser cannot load
 * (some networks / proxies block the platforms' image hosts in browsers) is
 * fetched once more through bailing. Costs a little server traffic, and only
 * for covers that actually failed.
 */
export function coverFallback(img: HTMLImageElement) {
	const onError = () => {
		const src = img.getAttribute('src') || '';
		if (!/^https?:\/\//i.test(src) || src.includes('/media/rcover?')) return;
		if (src.startsWith(location.origin) || (apiBase && src.startsWith(apiBase))) return;
		img.src = `${apiBase}/media/rcover?u=${encodeURIComponent(src)}`;
	};
	img.addEventListener('error', onError);
	return { destroy: () => img.removeEventListener('error', onError) };
}

// ---- online sources (P2 / P3) ------------------------------------------------------
/** A source-independent song / station reference (Go: sources.TrackRef). */
export interface TrackRef {
	source: string; // direct | radio | lx:kw ...
	id: string;
	title: string;
	artist: string;
	album: string;
	duration_ms: number;
	cover: string;
	extra?: Record<string, unknown>;
}
export interface SourceInfo {
	id: string;
	name: string;
	kind: 'local' | 'online' | 'radio' | 'lx';
	capabilities: string[];
	qualities?: string[];
}
export interface ResolveResult {
	url: string;
	format: string;
	mode: 'direct' | 'proxy';
	proxy_url?: string;
	cors?: boolean;
	expires_at?: number;
}
export interface SearchResult {
	items: TrackRef[];
	has_more: boolean;
}
export interface LxDecl {
	name: string;
	type: string;
	actions: string[];
	qualitys: string[];
}
export interface LxScript {
	id: string;
	file: string;
	name: string;
	description: string;
	version: string;
	author: string;
	homepage: string;
	enabled: boolean;
	status: 'loading' | 'ok' | 'error' | 'disabled';
	error?: string;
	update?: string;
	sources: Record<string, LxDecl>;
	size: number;
	mtime: number;
}
export interface LxState {
	enabled: boolean;
	dir?: string;
	scripts: LxScript[];
	platforms: { code: string; name: string; search: boolean; qualities: string[] }[];
}

// ---- shared types ----------------------------------------------------------------
/** Which halves of the app the server runs (config.yaml → features). */
export interface Features {
	photos: boolean;
	music: boolean;
}
/** A song or station snapshot stored in favourites / playlists. */
export type SavedItem = TrackRef & Partial<Track>;
export interface FavEntry {
	kind: 'track' | 'station';
	item: SavedItem;
	at: number;
}
export interface PlaylistInfo {
	id: number;
	name: string;
	count: number;
	first?: SavedItem;
	updated_at: number;
}
export interface Playlist {
	id: number;
	name: string;
	updated_at: number;
	items: SavedItem[];
}
export interface User {
	id: number;
	name: string;
	display_name: string;
	is_admin: boolean;
}
export interface Album {
	id: string;
	title: string;
	description: string;
	date: string;
	cover: string;
	count: number;
	color: string;
}
export interface Photo {
	id: string;
	name: string;
	w: number;
	h: number;
	color: string;
	taken?: number;
	ready: boolean;
}
export interface Track {
	id: string;
	title: string;
	artist: string;
	album: string;
	duration_ms: number;
	cover: string;
	color: string;
	format: string;
	folder: string;
	track_no?: number;
	year?: number;
}
export interface Group {
	name: string;
	count: number;
	cover: string;
}
export interface LyricLine {
	t: number;
	text: string;
	trans?: string;
}
export interface LyricsResult {
	status: 'ok' | 'none' | 'instrumental';
	source: string;
	synced: boolean;
	lines: LyricLine[];
}
export interface StatItem {
	kind: 'track' | 'album' | 'photo';
	source?: string;
	ref: string;
	title: string;
	sub?: string;
	cover?: string;
	album?: string;
	count?: number;
	at?: number;
}
export interface StatsSummary {
	recent_plays: StatItem[];
	top_plays: StatItem[];
	recent_views: StatItem[];
	top_views: StatItem[];
}
export interface TrafficStream {
	sid: string;
	category: string;
	ref: string;
	title: string;
	bytes: number;
	started_at: number;
	last_at: number;
}
export interface TrafficSummary {
	today: number;
	month: number;
	month_start: string;
	by_category: Record<string, number>;
	days: { day: string; bytes: number }[];
	quota_bytes: number;
	warn_percent: number;
	warn: boolean;
	over: boolean;
	recent_streams: TrafficStream[];
}
export interface Exif {
	make?: string;
	model?: string;
	lens?: string;
	focal?: string;
	fnumber?: string;
	exposure?: string;
	iso?: string;
	taken?: string;
	width?: number;
	height?: number;
	lat?: number;
	lon?: number;
}

