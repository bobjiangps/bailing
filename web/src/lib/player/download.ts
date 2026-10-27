// "Download to this device". Local songs come straight from bailing; online
// songs are resolved like for playback and then fetched through the signed
// relay, because a browser only honours "save as file" for same-origin
// links. That download therefore counts as server (relay) traffic.
import { apiBase, ApiError, type ResolveResult } from '$lib/api/client';
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

/** LX quality names, best last. */
export const QUALITY_ORDER = ['128k', '192k', '320k', 'flac', 'flac24bit', 'hires', 'atmos', 'master'];
const rank = (q: string) => {
	const i = QUALITY_ORDER.indexOf(q);
	return i < 0 ? -1 : i;
};

/** Online songs where a quality can be picked (LX sources). */
export const hasQualities = (q: { source: string }) => q.source.startsWith('lx:');

export interface QualityOption {
	q: string;
	/** file size as the platform reports it, e.g. "7.58MB" ('' = unknown) */
	size: string;
}

/**
 * Qualities this song exists in, worst first, from the search result's
 * LX musicInfo (`_types: {"320k": {size: "7.58MB"}}`). Songs saved before
 * sizes were recorded fall back to the usual four, without sizes.
 */
export function qualityOptions(item: { extra?: Record<string, unknown> }): QualityOption[] {
	const types = item.extra?._types;
	const out: QualityOption[] = [];
	if (types && typeof types === 'object') {
		for (const [q, v] of Object.entries(types as Record<string, unknown>)) {
			const size = v && typeof v === 'object' ? String((v as { size?: unknown }).size ?? '') : '';
			out.push({ q, size });
		}
	}
	if (!out.length) return ['128k', '320k', 'flac', 'flac24bit'].map((q) => ({ q, size: '' }));
	return out.sort((a, b) => rank(a.q) - rank(b.q));
}

/** The preferred quality, or the best one below it that the song has. */
export function defaultQuality(opts: QualityOption[], want: string): string {
	if (opts.some((o) => o.q === want)) return want;
	const below = opts.filter((o) => rank(o.q) >= 0 && rank(o.q) <= rank(want));
	return (below.at(-1) ?? opts[0])?.q ?? want;
}

/**
 * The file behind the chosen quality is not that quality (an error body of a
 * few bytes, far from the listed size, or a lower quality): nothing was
 * downloaded. `detail` is the server's per-script explanation.
 */
export class BadQualityError extends Error {
	detail: string;
	constructor(detail: string) {
		super(detail);
		this.detail = detail;
	}
}

/**
 * One step of resolving an online song, streamed live by the server: which
 * source script is tried, why one was rejected, which one delivered.
 */
export interface SourceStep {
	script: string;
	quality: string;
	status: 'trying' | 'bad' | 'ok';
	/** why a script was rejected */
	kind?: 'no_quality' | 'error' | 'dead' | 'mismatch' | 'lower';
	detail?: string;
	/** the file's real size / the size the platform lists for the quality */
	bytes?: number;
	expected?: number;
	/** the quality the file's size looks like */
	got?: string;
	cached?: boolean;
}

/**
 * POST /api/play/resolve with progress: the answer is NDJSON, one
 * {"type":"step"} line per source script step, then {"type":"done"} or
 * {"type":"error"}.
 */
async function resolveWithSteps(
	body: Record<string, unknown>,
	onStep: (s: SourceStep) => void,
	signal?: AbortSignal
): Promise<ResolveResult> {
	const res = await fetch(apiBase + '/api/play/resolve', {
		method: 'POST',
		credentials: 'include',
		signal,
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ ...body, progress: true })
	});
	if (!res.ok || !res.body) {
		let code = 'error';
		let msg = res.statusText;
		try {
			const j = await res.json();
			code = j.code ?? code;
			msg = j.error ?? msg;
		} catch {
			/* not JSON */
		}
		throw new ApiError(res.status, code, msg);
	}
	const reader = res.body.getReader();
	const dec = new TextDecoder();
	let buf = '';
	for (;;) {
		const { value, done } = await reader.read();
		if (value) buf += dec.decode(value, { stream: true });
		let nl: number;
		while ((nl = buf.indexOf('\n')) >= 0) {
			const line = buf.slice(0, nl).trim();
			buf = buf.slice(nl + 1);
			if (!line) continue;
			const m = JSON.parse(line);
			if (m.type === 'step') onStep(m.step as SourceStep);
			else if (m.type === 'done') return m.result as ResolveResult;
			else if (m.type === 'error') throw new ApiError(m.status ?? 502, m.code ?? 'error', m.error ?? '');
		}
		if (done) throw new Error('connection closed');
	}
}

export interface DownloadResult {
	/** the quality saved, when lower than the one asked for */
	lower?: string;
	/** the source script the file comes from */
	script?: string;
	/** scripts that were tried and rejected before it */
	failed?: string[];
}

/**
 * Starts the download; throws a short message when it cannot. Returns the
 * source script the file comes from (and the ones rejected before it), and
 * the quality that is being saved when it is lower than the one asked for
 * (the scripts / platform had nothing better), so the caller can say so.
 * onStep is called live for every source script tried.
 *
 * strict (the download dialog, where the user chose a quality on purpose):
 * the server checks the real file against the size the platform lists and
 * refuses anything else with BadQualityError, instead of saving a broken or
 * lower-quality file.
 */
export async function download(
	q: QueueItem,
	quality = prefs.lxQuality,
	strict = false,
	signal?: AbortSignal,
	onStep?: (s: SourceStep) => void
): Promise<DownloadResult> {
	if (q.source === 'local') {
		save(`${apiBase}/media/track/${encodeURIComponent(q.id)}?dl=1`);
		return {};
	}
	let r: ResolveResult;
	const want = quality;
	let script = '';
	const failed: string[] = [];
	try {
		r = await resolveWithSteps(
			{ source: q.source, track: toRef(q), quality: want, strict },
			(s) => {
				if (s.status === 'ok') script = s.script;
				else if (s.status === 'bad' && !failed.includes(s.script)) failed.push(s.script);
				onStep?.(s);
			},
			signal
		);
	} catch (e) {
		if (signal?.aborted) throw e;
		if (e instanceof ApiError && e.code === 'bad_quality') throw new BadQualityError(e.message);
		throw new Error(e instanceof ApiError ? e.message : String(e));
	}
	if (r.format === 'hls') throw new Error('HLS stream');
	// never save a different quality than the one chosen in the dialog
	if (strict && r.quality && r.quality !== want) throw new BadQualityError(`got ${r.quality} instead of ${want}`);
	if (!r.proxy_url) throw new Error('relay disabled (sources.proxy.enabled)');
	const name = q.artist ? `${q.artist} - ${q.title}` : q.title;
	save(`${apiBase}${r.proxy_url}&dl=1&name=${encodeURIComponent(name)}`);
	const out: DownloadResult = { script, failed: failed.filter((f) => f !== script) };
	const got = r.quality ? QUALITY_ORDER.indexOf(r.quality) : -1;
	if (got >= 0 && got < QUALITY_ORDER.indexOf(want)) out.lower = r.quality;
	return out;
}

