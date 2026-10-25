// "Download to this device". Local songs come straight from bailing; online
// songs are resolved like for playback and then fetched through the signed
// relay, because a browser only honours "save as file" for same-origin
// links. That download therefore counts as server (relay) traffic.
import { api, apiBase, ApiError, type ResolveResult } from '$lib/api/client';
import { prefs } from '$lib/stores/prefs.svelte';
import { toRef, type QueueItem } from './player.svelte';

/** Radio and other live streams cannot be saved as a file. */
export const canDownload = (q: { source: string; live?: boolean }) => q.source !== 'radio' && !q.live;

function save(href: string) {
	const a = document.createElement('a');
	a.href = href;
	a.download = '';
	a.rel = 'noopener';
	document.body.appendChild(a);
	a.click();
	a.remove();
}

/** Starts the download; throws a short message when it cannot. */
export async function download(q: QueueItem): Promise<void> {
	if (q.source === 'local') {
		save(`${apiBase}/media/track/${encodeURIComponent(q.id)}?dl=1`);
		return;
	}
	let r: ResolveResult;
	try {
		r = await api<ResolveResult>('/api/play/resolve', {
			body: { source: q.source, track: toRef(q), quality: prefs.lxQuality }
		});
	} catch (e) {
		throw new Error(e instanceof ApiError ? e.message : String(e));
	}
	if (r.format === 'hls') throw new Error('HLS stream');
	if (!r.proxy_url) throw new Error('relay disabled (sources.proxy.enabled)');
	const name = q.artist ? `${q.artist} - ${q.title}` : q.title;
	save(`${apiBase}${r.proxy_url}&dl=1&name=${encodeURIComponent(name)}`);
}

