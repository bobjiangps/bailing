<script lang="ts">
	// Online: search LX Music platforms (playback through the custom-source
	// scripts the admin installed) or paste any audio / m3u8 link.
	import { base } from '$app/paths';
	import { api, ApiError, coverFallback, type SearchResult, type SourceInfo, type TrackRef } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { fromRef, player } from '$lib/player/player.svelte';
	import { toSaved } from '$lib/stores/collections.svelte';
	import AddToPlaylist from './AddToPlaylist.svelte';
	import DownloadButton from './DownloadButton.svelte';
	import FavButton from './FavButton.svelte';
	import { prefs } from '$lib/stores/prefs.svelte';
	import { session } from '$lib/stores/session.svelte';
	import { formatTime } from '$lib/util/format';

	const HIST_KEY = 'bl.urlHistory';
	const QUALITIES = ['128k', '320k', 'flac', 'flac24bit'];

	let mode = $state<'lx' | 'url'>('lx');
	let srcs = $state<SourceInfo[]>([]);
	let platform = $state('');
	let q = $state('');
	let items = $state<TrackRef[]>([]);
	let hasMore = $state(false);
	let loading = $state(false);
	let err = $state('');
	let searched = $state(false);
	let page = 1;
	let reqId = 0;

	let url = $state('');
	let title = $state('');
	let urlErr = $state('');
	let history = $state<TrackRef[]>(readHistory());

	const lxSources = $derived(srcs.filter((s) => s.kind === 'lx' && s.capabilities.includes('search')));
	const cur = $derived(lxSources.find((s) => s.id === platform));
	const playable = $derived(srcs.some((s) => s.kind === 'lx' && s.capabilities.includes('resolve')));
	const qualities = $derived(cur?.qualities?.length ? cur.qualities : QUALITIES);

	$effect(() => {
		api<{ sources: SourceInfo[] }>('/api/sources')
			.then((r) => {
				srcs = r.sources;
				const lx = r.sources.filter((s) => s.kind === 'lx' && s.capabilities.includes('search'));
				// prefer a platform some script can actually resolve
				const best = lx.find((s) => s.capabilities.includes('resolve')) ?? lx[0];
				if (best && !platform) platform = best.id;
			})
			.catch(() => {});
	});

	async function search(reset: boolean) {
		if (!platform || !q.trim()) return;
		const my = ++reqId;
		if (reset) page = 1;
		loading = true;
		err = '';
		try {
			const r = await api<SearchResult>(
				`/api/sources/${encodeURIComponent(platform)}/search?q=${encodeURIComponent(q.trim())}&page=${page}`
			);
			if (my !== reqId) return;
			items = reset ? r.items : [...items, ...r.items];
			hasMore = r.has_more;
			searched = true;
		} catch (e) {
			if (my === reqId) err = e instanceof ApiError ? e.message : String(e);
		} finally {
			if (my === reqId) loading = false;
		}
	}

	function submit(e: Event) {
		e.preventDefault();
		search(true);
	}

	function pickPlatform(id: string) {
		platform = id;
		if (q.trim()) search(true);
	}

	// ---- links ----
	function readHistory(): TrackRef[] {
		try {
			const v = JSON.parse(localStorage.getItem(HIST_KEY) || '[]');
			return Array.isArray(v) ? v : [];
		} catch {
			return [];
		}
	}
	function linkRef(): TrackRef | null {
		const u = url.trim();
		if (!/^https?:\/\/\S+$/i.test(u)) {
			urlErr = t('online.invalid');
			return null;
		}
		urlErr = '';
		let name = title.trim();
		if (!name) {
			try {
				const p = new URL(u);
				name = decodeURIComponent(p.pathname.split('/').pop() || '').replace(/\.[a-z0-9]+$/i, '') || p.host;
			} catch {
				name = u;
			}
		}
		const ref: TrackRef = { source: 'direct', id: u, title: name, artist: '', album: '', duration_ms: 0, cover: '' };
		history = [ref, ...history.filter((h) => h.id !== u)].slice(0, 20);
		try {
			localStorage.setItem(HIST_KEY, JSON.stringify(history));
		} catch {
			/* quota */
		}
		return ref;
	}
	function playLink() {
		const r = linkRef();
		if (r) player.playList([fromRef(r)], 0);
	}
	function queueLink() {
		const r = linkRef();
		if (r) player.playNext(fromRef(r));
	}
	function removeHist(id: string) {
		history = history.filter((h) => h.id !== id);
		localStorage.setItem(HIST_KEY, JSON.stringify(history));
	}
</script>

<div class="online">
	<div class="seg">
		<button class:on={mode === 'lx'} onclick={() => (mode = 'lx')}><Icon name="globe" size={14} />{t('online.lx')}</button>
		<button class:on={mode === 'url'} onclick={() => (mode = 'url')}><Icon name="link" size={14} />{t('online.url')}</button>
	</div>

	{#if mode === 'lx'}
		{#if !lxSources.length}
			<p class="muted note">{t('online.disabled')}</p>
		{:else}
			<div class="plats">
				{#each lxSources as s (s.id)}
					<button class:on={platform === s.id} class:dim={!s.capabilities.includes('resolve')} onclick={() => pickPlatform(s.id)}>
						{s.name}
					</button>
				{/each}
				<label class="q">
					{t('online.quality')}
					<select value={prefs.lxQuality} onchange={(e) => prefs.set('lxQuality', e.currentTarget.value)}>
						{#each qualities as ql (ql)}<option value={ql}>{ql}</option>{/each}
					</select>
				</label>
			</div>
			{#if !playable}
				<p class="note warn">
					{t('online.noScript')}
					{#if session.user?.is_admin}<a href="{base}/settings#lx">{t('online.toSettings')}</a>{/if}
				</p>
			{/if}
			<form class="search" onsubmit={submit}>
				<Icon name="search" size={15} />
				<input type="search" bind:value={q} placeholder={t('online.search')} aria-label={t('online.search')} />
				<button class="btn primary" type="submit" disabled={loading || !q.trim()}>{loading ? '…' : t('common.search')}</button>
			</form>

			{#if err}
				<p class="muted center">{t('online.searchFail', { e: err })}</p>
			{:else if searched && !items.length && !loading}
				<p class="muted center art">{t('online.empty')}</p>
			{:else if items.length}
				<ol class="tracks">
					{#each items as it, i (it.id + ':' + i)}
						{@const isCur = player.current?.source === it.source && player.current.id === it.id}
						<li class:cur={isCur}>
							<button class="row" onclick={() => player.playList(items.map(fromRef), i)}>
								<span class="idx num muted">{i + 1}</span>
								{#if it.cover}<img src={it.cover} alt="" loading="lazy" use:coverFallback />{:else}<span class="ph"></span>{/if}
								<span class="tt">
									<span class="title">{it.title}</span>
									<span class="sub muted">{it.artist}{#if it.album} · {it.album}{/if}</span>
								</span>
								<span class="dur num muted">{it.duration_ms ? formatTime(it.duration_ms / 1000) : ''}</span>
							</button>
							<FavButton item={() => toSaved(it)} size={15} />
							<AddToPlaylist items={() => [toSaved(it)]} size={15} />
							<DownloadButton item={() => fromRef(it)} size={15} />
							<button class="icon-btn" title={t('music.playNext')} onclick={() => player.playNext(fromRef(it))}>
								<Icon name="queue" size={15} />
							</button>
						</li>
					{/each}
				</ol>
				{#if hasMore}
					<p class="center-s">
						<button class="btn" disabled={loading} onclick={() => { page++; search(false); }}>
							{loading ? t('common.loading') : t('online.more')}
						</button>
					</p>
				{/if}
			{/if}
			<p class="hint muted">{t('online.risk')}</p>
		{/if}
	{:else}
		<div class="link">
			<input class="field" type="url" bind:value={url} placeholder={t('online.urlPlaceholder')} aria-label={t('online.urlPlaceholder')} />
			<input class="field" type="text" bind:value={title} placeholder={t('online.titlePlaceholder')} aria-label={t('online.titlePlaceholder')} />
			<div class="acts">
				<button class="btn primary" onclick={playLink}><Icon name="play" size={14} />{t('online.playNow')}</button>
				<button class="btn" onclick={queueLink}><Icon name="queue" size={14} />{t('online.addQueue')}</button>
				{#if urlErr}<span class="err">{urlErr}</span>{/if}
			</div>
			<p class="hint muted">{t('online.urlHint')}</p>
		</div>
		{#if history.length}
			<h4 class="muted">{t('online.history')}</h4>
			<ul class="hist">
				{#each history as h, i (h.id)}
					<li>
						<button class="row" onclick={() => player.playList(history.map(fromRef), i)}>
							<Icon name="link" size={14} />
							<span class="tt"><span class="title">{h.title}</span><span class="sub muted">{h.id}</span></span>
						</button>
						<button class="icon-btn" aria-label="remove" onclick={() => removeHist(h.id)}><Icon name="x" size={14} /></button>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</div>

<style>
	.seg {
		display: inline-flex;
		gap: 2px;
		padding: 2px;
		margin: 0 6px 10px;
		border-radius: 999px;
		background: var(--accent-soft);
	}
	.seg button {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		padding: 3px 12px;
		border-radius: 999px;
		font-size: 13px;
	}
	.seg button.on {
		background: var(--surface);
		box-shadow: 0 2px 8px -4px var(--shadow);
	}
	.plats {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px;
		padding: 0 6px 8px;
	}
	.plats > button {
		padding: 3px 12px;
		border-radius: 999px;
		font-size: 13px;
		color: var(--text-muted);
		border: 1px solid var(--line);
	}
	.plats > button.on {
		color: var(--text);
		background: var(--accent-soft);
		border-color: transparent;
	}
	.plats > button.dim {
		opacity: 0.6;
	}
	.q {
		margin-left: auto;
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 12.5px;
		color: var(--text-muted);
	}
	select {
		padding: 3px 8px;
		border-radius: 999px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text);
	}
	.note {
		margin: 0 6px 10px;
		font-size: 12.5px;
		line-height: 1.6;
	}
	.note.warn {
		padding: 8px 10px;
		border-radius: 10px;
		background: color-mix(in oklch, var(--accent) 14%, transparent);
	}
	.note a {
		border-bottom: 1px dashed currentColor;
		margin-left: 4px;
	}
	.search {
		display: flex;
		align-items: center;
		gap: 6px;
		margin: 0 6px 10px;
		padding: 3px 4px 3px 12px;
		border-radius: 999px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text-muted);
	}
	.search input {
		flex: 1;
		border: 0;
		background: none;
		outline: none;
		font-size: 14px;
	}
	.search .btn {
		font-size: 13px;
		padding: 0.35em 1em;
	}
	.tracks,
	.hist {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.tracks li,
	.hist li {
		display: flex;
		align-items: center;
		border-radius: var(--radius-s);
		transition: background var(--dur-fast);
	}
	.tracks li:hover,
	.hist li:hover {
		background: var(--accent-soft);
	}
	.tracks li.cur .title {
		color: var(--accent);
	}
	.row {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 6px 8px;
		text-align: left;
	}
	.idx {
		width: 24px;
		text-align: right;
		font-size: 12px;
		flex: none;
	}
	.row img,
	.ph {
		width: 36px;
		height: 36px;
		border-radius: 6px;
		object-fit: cover;
		flex: none;
		background: var(--accent-soft);
	}
	.tt {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.title,
	.sub {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.title {
		font-size: 14.5px;
	}
	.sub {
		font-size: 12px;
	}
	.dur {
		font-size: 12.5px;
		width: 44px;
		text-align: right;
	}
	.link {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 0 6px;
	}
	.field {
		padding: 8px 12px;
		border-radius: 12px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text);
		font-size: 14px;
	}
	.acts {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.acts .btn {
		font-size: 13px;
	}
	.err {
		color: var(--accent);
		font-size: 13px;
	}
	h4 {
		margin: 16px 8px 4px;
		font-weight: 400;
		font-size: 13px;
	}
	.center {
		text-align: center;
		margin: 6vh auto;
	}
	.center-s {
		text-align: center;
	}
	.hint {
		font-size: 11.5px;
		margin: 10px 8px 0;
		line-height: 1.6;
	}
</style>

