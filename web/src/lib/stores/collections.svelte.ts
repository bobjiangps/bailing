import {
	api,
	type FavEntry,
	type Playlist,
	type PlaylistInfo,
	type SavedItem,
	type Track,
	type TrackRef
} from '$lib/api/client';

/** Snapshot of a queue item / track / station for storing on the server. */
export function toSaved(x: TrackRef | (Track & { source?: string; extra?: Record<string, unknown> })): SavedItem {
	const s: SavedItem = {
		source: x.source || 'local',
		id: x.id,
		title: x.title,
		artist: x.artist || '',
		album: x.album || '',
		duration_ms: x.duration_ms || 0,
		cover: x.cover || ''
	};
	if ('extra' in x && x.extra) s.extra = x.extra;
	if (s.source === 'local') {
		const t = x as Track;
		s.color = t.color || '';
		s.format = t.format || '';
		s.folder = t.folder || '';
	}
	return s;
}

const key = (source: string, id: string) => source + '|' + id;

// Radio favourites used to live only in this browser; they are uploaded
// once after login and then removed locally.
const OLD_RADIO_FAV = 'bl.radioFav';

/**
 * Favourites (songs + stations) and playlists of the signed-in user.
 * Stored on the server, so they follow the user to every device.
 */
class Collections {
	favs = $state<FavEntry[]>([]);
	playlists = $state<PlaylistInfo[]>([]);
	loaded = $state(false);

	private favKeys = $derived(new Set(this.favs.map((f) => key(f.item.source, f.item.id))));
	tracks = $derived(this.favs.filter((f) => f.kind === 'track').map((f) => f.item));
	stations = $derived(this.favs.filter((f) => f.kind === 'station').map((f) => f.item));

	async load() {
		try {
			await this.migrateOld();
			const [f, p] = await Promise.all([
				api<{ items: FavEntry[] }>('/api/favorites'),
				api<{ playlists: PlaylistInfo[] }>('/api/playlists')
			]);
			this.favs = f.items;
			this.playlists = p.playlists;
			this.loaded = true;
		} catch {
			/* offline / feature disabled */
		}
	}

	reset() {
		this.favs = [];
		this.playlists = [];
		this.loaded = false;
	}

	private async migrateOld() {
		let old: TrackRef[] = [];
		try {
			old = JSON.parse(localStorage.getItem(OLD_RADIO_FAV) || '[]');
		} catch {
			/* ignore */
		}
		if (!Array.isArray(old) || !old.length) return;
		await api('/api/favorites', { body: { kind: 'station', items: old.map(toSaved) } });
		localStorage.removeItem(OLD_RADIO_FAV);
	}

	// ---- favourites ---------------------------------------------------------------

	isFav(source: string, id: string) {
		return this.favKeys.has(key(source, id));
	}

	async toggleFav(item: SavedItem, kind: 'track' | 'station' = item.source === 'radio' ? 'station' : 'track') {
		const k = key(item.source, item.id);
		if (this.favKeys.has(k)) {
			const before = this.favs;
			this.favs = this.favs.filter((f) => key(f.item.source, f.item.id) !== k);
			try {
				await api('/api/favorites/remove', { body: { source: item.source, id: item.id } });
			} catch (e) {
				this.favs = before;
				throw e;
			}
		} else {
			const entry: FavEntry = { kind, item, at: Date.now() };
			this.favs = [entry, ...this.favs];
			try {
				await api('/api/favorites', { body: { kind, item } });
			} catch (e) {
				this.favs = this.favs.filter((f) => f !== entry);
				throw e;
			}
		}
	}

	// ---- playlists ------------------------------------------------------------------

	async refreshPlaylists() {
		const p = await api<{ playlists: PlaylistInfo[] }>('/api/playlists');
		this.playlists = p.playlists;
	}

	async create(name: string, items: SavedItem[] = []) {
		const r = await api<{ id: number; name: string }>('/api/playlists', { body: { name, items } });
		await this.refreshPlaylists();
		return r.id;
	}

	get(id: number) {
		return api<Playlist>(`/api/playlists/${id}`);
	}

	async add(id: number, items: SavedItem[]) {
		const r = await api<{ added: number; skipped: number }>(`/api/playlists/${id}/items`, { body: { items } });
		await this.refreshPlaylists();
		return r;
	}

	async save(id: number, patch: { name?: string; items?: SavedItem[] }) {
		await api(`/api/playlists/${id}`, { method: 'PUT', body: patch });
		await this.refreshPlaylists();
	}

	async remove(id: number) {
		await api(`/api/playlists/${id}`, { method: 'DELETE' });
		this.playlists = this.playlists.filter((p) => p.id !== id);
	}
}

export const collections = new Collections();

