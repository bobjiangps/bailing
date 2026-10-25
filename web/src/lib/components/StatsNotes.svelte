<script lang="ts">
	// The small "sticky notes" strip on the home page (members only).
	// Collapsed to one line by default so it never competes with the scenery.
	import { base } from '$app/paths';
	import { api, coverUrl, photoUrl, type StatItem, type StatsSummary } from '$lib/api/client';
	import Icon from './Icon.svelte';
	import { t } from '$lib/i18n';
	import { player, fromRef, fromTrack, recallOnline } from '$lib/player/player.svelte';
	import type { Track } from '$lib/api/client';

	type Tab = 'recent_plays' | 'top_plays' | 'recent_views' | 'top_views';
	const tabs: { key: Tab; label: string }[] = [
		{ key: 'recent_plays', label: 'notes.recentPlays' },
		{ key: 'top_plays', label: 'notes.topPlays' },
		{ key: 'recent_views', label: 'notes.recentViews' },
		{ key: 'top_views', label: 'notes.topViews' }
	];

	// How many entries each list shows (the API allows up to 20).
	const LIMIT = 10;
	const OPEN_KEY = 'bl.notesOpen';

	let data = $state<StatsSummary | null>(null);
	let tab = $state<Tab>('recent_plays');
	// Expanded by default; only a deliberate collapse is remembered.
	let open = $state(readOpen());

	function readOpen() {
		try {
			return localStorage.getItem(OPEN_KEY) !== '0';
		} catch {
			return true;
		}
	}

	function setOpen(v: boolean) {
		open = v;
		try {
			localStorage.setItem(OPEN_KEY, v ? '1' : '0');
		} catch {
			/* private mode */
		}
	}

	let loadedAt = 0;
	function load() {
		loadedAt = Date.now();
		api<StatsSummary>(`/api/stats/summary?limit=${LIMIT}`)
			.then((r) => (data = r))
			.catch(() => {});
	}

	// Load on every mount (coming back from Music / Albums), and again when the
	// tab becomes visible or a play has just been counted while we sit here.
	$effect(() => {
		load();
		const onVisible = () => {
			if (document.visibilityState === 'visible' && Date.now() - loadedAt > 5000) load();
		};
		const onCounted = () => setTimeout(load, 800);
		document.addEventListener('visibilitychange', onVisible);
		window.addEventListener('bl:stats', onCounted);
		return () => {
			document.removeEventListener('visibilitychange', onVisible);
			window.removeEventListener('bl:stats', onCounted);
		};
	});

	function pick(k: Tab) {
		tab = k;
		setOpen(true);
		if (Date.now() - loadedAt > 20000) load(); // pick up plays counted since
	}

	const items = $derived(data ? data[tab] : []);

	async function playItem(it: StatItem) {
		if (it.source && it.source !== 'local') {
			// online: use the full item remembered by the player, or rebuild
			// a minimal one (radio resolves by uuid, direct by URL)
			const ref = recallOnline(it.source, it.ref) ?? {
				source: it.source,
				id: it.ref,
				title: it.title,
				artist: it.sub ?? '',
				album: '',
				duration_ms: 0,
				cover: it.cover ?? ''
			};
			player.playList([fromRef(ref)], 0);
			return;
		}
		try {
			const r = await api<{ tracks: Track[] }>(`/api/music/tracks?q=&limit=5000`);
			const list = r.tracks.map(fromTrack);
			const i = list.findIndex((x) => x.id === it.ref);
			if (i >= 0) player.playList(list, i);
		} catch {
			/* ignore */
		}
	}

	function thumb(it: StatItem) {
		if (it.kind === 'track') return it.cover ? coverUrl(it.cover) : '';
		return it.cover ? photoUrl(it.cover, 's') : '';
	}
</script>

<section class="notes glass" class:open>
	<div class="tabs">
		{#each tabs as tb (tb.key)}
			<button
				class:on={tab === tb.key}
				onclick={() => pick(tb.key)}>{t(tb.label)}</button
			>
		{/each}
		<button class="toggle icon-btn" aria-expanded={open} aria-label="toggle" onclick={() => setOpen(!open)}>
			<Icon name="up" size={16} />
		</button>
	</div>
	{#if open}
		<ul>
			{#each items as it (it.kind + it.ref)}
				<li>
					{#if it.kind === 'track'}
						<button class="item" onclick={() => playItem(it)}>
							{#if thumb(it)}<img src={thumb(it)} alt="" class="round" />{:else}<span class="ph round"></span>{/if}
							<span class="txt"><span class="title">{it.title}</span><span class="sub">{it.sub}</span></span>
							{#if it.count}<span class="cnt num">{t('notes.times', { n: it.count })}</span>{/if}
						</button>
					{:else}
						<a class="item" href="{base}/albums?open={it.album}">
							{#if thumb(it)}<img src={thumb(it)} alt="" />{:else}<span class="ph"></span>{/if}
							<span class="txt"><span class="title">{it.title}</span><span class="sub">{it.sub ?? ''}</span></span>
							{#if it.count}<span class="cnt num">{t('notes.times', { n: it.count })}</span>{/if}
						</a>
					{/if}
				</li>
			{:else}
				<li class="empty muted art">{data ? t('notes.empty') : t('common.loading')}</li>
			{/each}
		</ul>
		{#if tab === 'recent_plays' || tab === 'top_plays'}
			<p class="rule muted">{t('notes.playRule')}</p>
		{/if}
	{/if}
</section>

<style>
	.notes {
		width: min(720px, 100%);
		margin: 0 auto;
		border-radius: var(--radius-l);
		padding: 6px 8px;
		box-shadow: 0 14px 40px -28px var(--shadow);
		transition: padding var(--dur-base) var(--ease-soft);
	}
	.tabs {
		display: flex;
		align-items: center;
		gap: 4px;
		overflow-x: auto;
		scrollbar-width: none;
	}
	.tabs > button:not(.toggle) {
		padding: 4px 12px;
		border-radius: 999px;
		font-size: 13px;
		color: var(--text-muted);
		white-space: nowrap;
		transition: background var(--dur-fast), color var(--dur-fast);
	}
	.tabs > button.on {
		color: var(--text);
		background: var(--accent-soft);
	}
	.toggle {
		margin-left: auto;
		width: 30px;
		height: 30px;
		transform: rotate(0deg);
		transition: transform var(--dur-base) var(--ease-soft);
	}
	.open .toggle {
		transform: rotate(180deg);
	}
	ul {
		list-style: none;
		margin: 6px 0 2px;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
		gap: 4px;
		max-height: 232px;
		overflow-y: auto;
		animation: slide var(--dur-base) var(--ease-soft);
	}
	.item {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 5px 8px;
		border-radius: 10px;
		text-align: left;
	}
	.item:hover {
		background: var(--accent-soft);
	}
	img,
	.ph {
		width: 32px;
		height: 32px;
		border-radius: 6px;
		object-fit: cover;
		flex: none;
		background: var(--accent-soft);
	}
	.round {
		border-radius: 50%;
	}
	.txt {
		min-width: 0;
		display: flex;
		flex-direction: column;
		line-height: 1.3;
	}
	.title,
	.sub {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.title {
		font-size: 13px;
	}
	.sub {
		font-size: 11px;
		color: var(--text-muted);
	}
	.cnt {
		margin-left: auto;
		font-size: 11px;
		color: var(--text-muted);
	}
	.empty {
		padding: 10px;
		font-size: 14px;
	}
	.rule {
		margin: 2px 8px 2px;
		font-size: 11px;
	}
	@keyframes slide {
		from {
			opacity: 0;
			transform: translateY(8px);
		}
	}
</style>
