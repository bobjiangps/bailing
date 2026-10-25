<script lang="ts">
	// Internet radio: browse the open Radio-Browser directory by name,
	// region and style; star stations to keep them in "Favourites"
	// (stored on the server, so they follow you to every device).
	import { api, type TrackRef } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { fromRef, player } from '$lib/player/player.svelte';
	import { collections, toSaved } from '$lib/stores/collections.svelte';
	import AddToPlaylist from './AddToPlaylist.svelte';

	const TAGS = ['chillout', 'lofi', 'jazz', 'classical', 'ambient', 'pop', 'rock', 'news'];

	type Country = { name: string; iso_3166_1: string; stationcount: number };

	let view = $state<'all' | 'fav'>('all');
	let q = $state('');
	let country = $state('');
	let tag = $state('');
	/** only stations that can play over https: never relayed by the server */
	let httpsOnly = $state(false);
	let items = $state<TrackRef[]>([]);
	let hasMore = $state(false);
	let loading = $state(false);
	let failed = $state(false);
	let countries = $state<Country[]>([]);
	const favs = $derived(collections.stations as TrackRef[]);
	let page = 1;
	let reqId = 0;

	const isFav = (r: TrackRef) => collections.isFav(r.source, r.id);
	function toggleFav(r: TrackRef) {
		collections.toggleFav(toSaved(r), 'station').catch(() => {});
	}
	/** stream address is https (or can be played without the relay) */
	const isSecure = (r: TrackRef) => String(r.extra?.url ?? '').startsWith('https:');

	$effect(() => {
		api<{ countries: Country[] }>('/api/radio/countries')
			.then((r) => (countries = r.countries.slice(0, 90)))
			.catch(() => {});
	});

	async function search(reset: boolean) {
		const my = ++reqId;
		if (reset) page = 1;
		loading = true;
		failed = false;
		try {
			const p = new URLSearchParams({ page: String(page) });
			if (q.trim()) p.set('q', q.trim());
			if (country) p.set('country', country);
			if (tag) p.set('tag', tag);
			if (httpsOnly) p.set('https', '1');
			const r = await api<{ items: TrackRef[]; has_more: boolean }>(`/api/radio/stations?${p}`);
			if (my !== reqId) return;
			items = reset ? r.items : [...items, ...r.items.filter((x) => !items.some((y) => y.id === x.id))];
			hasMore = r.has_more;
		} catch {
			if (my === reqId) failed = true;
		} finally {
			if (my === reqId) loading = false;
		}
	}

	function more() {
		page++;
		search(false);
	}

	$effect(() => {
		void q;
		void country;
		void tag;
		void httpsOnly;
		const h = setTimeout(() => search(true), q ? 350 : 0);
		return () => clearTimeout(h);
	});

	const list = $derived(view === 'fav' ? favs : items);

	function play(i: number) {
		player.playList(list.map(fromRef), i);
	}

	function meta(r: TrackRef) {
		const e = r.extra ?? {};
		const parts: string[] = [];
		if (e.codec) parts.push(String(e.codec));
		if (e.bitrate) parts.push(`${e.bitrate}k`);
		if (e.hls) parts.push('HLS');
		return parts.join(' · ');
	}
</script>

<div class="radio">
	<div class="bar">
		<div class="seg">
			<button class:on={view === 'all'} onclick={() => (view = 'all')}>{t('radio.all')}</button>
			<button class:on={view === 'fav'} onclick={() => (view = 'fav')}>
				<Icon name="star" size={13} />{t('radio.favs')}{#if favs.length} · {favs.length}{/if}
			</button>
		</div>
		{#if view === 'all'}
			<label class="search">
				<Icon name="search" size={15} />
				<input type="search" bind:value={q} placeholder={t('radio.search')} aria-label={t('radio.search')} />
			</label>
			<select bind:value={country} aria-label={t('radio.allCountries')}>
				<option value="">{t('radio.allCountries')}</option>
				{#each countries as c (c.iso_3166_1)}
					<option value={c.iso_3166_1}>{c.name} ({c.stationcount})</option>
				{/each}
			</select>
		{/if}
	</div>

	{#if view === 'all'}
		<div class="tags">
			<button class:on={tag === ''} onclick={() => (tag = '')}>{t('radio.anyTag')}</button>
			{#each TAGS as tg (tg)}
				<button class:on={tag === tg} onclick={() => (tag = tag === tg ? '' : tg)}>{tg}</button>
			{/each}
			<label class="https" title={t('radio.httpsOnlyHint')}>
				<input type="checkbox" bind:checked={httpsOnly} />{t('radio.httpsOnly')}
			</label>
		</div>
	{/if}

	{#if failed && view === 'all'}
		<p class="muted center">{t('radio.error')} <button class="btn" onclick={() => search(true)}>{t('common.retry')}</button></p>
	{:else if !list.length && !loading}
		<p class="muted center art">{view === 'fav' ? t('radio.favEmpty') : t('radio.empty')}</p>
	{:else}
		<ul class="stations">
			{#each list as st, i (st.id)}
				{@const isCur = player.current?.source === 'radio' && player.current.id === st.id}
				<li class:cur={isCur}>
					<button class="row" onclick={() => play(i)}>
						<span class="logo">
							{#if st.cover}
								<img src={st.cover} alt="" loading="lazy" onerror={(e) => ((e.currentTarget as HTMLElement).style.display = 'none')} />
							{/if}
							<Icon name="radio" size={18} />
						</span>
						<span class="tt">
							<span class="title">
								{#if isCur && player.playing}<span class="dot"></span>{/if}{st.title}
							</span>
							<span class="sub muted">{st.artist}</span>
						</span>
						<span class="meta muted">
							{#if !isSecure(st)}<span class="relay-tag" title={t('radio.mayRelay')}>http</span>{/if}
							{meta(st)}
						</span>
					</button>
					<AddToPlaylist items={() => [toSaved(st)]} />
					<button class="icon-btn fav" class:on={isFav(st)} title={isFav(st) ? t('radio.unfav') : t('radio.fav')} onclick={() => toggleFav(st)}>
						<Icon name={isFav(st) ? 'starFill' : 'star'} size={16} />
					</button>
				</li>
			{/each}
		</ul>
		{#if view === 'all' && hasMore}
			<p class="center-s"><button class="btn" disabled={loading} onclick={more}>{loading ? t('common.loading') : t('radio.more')}</button></p>
		{/if}
	{/if}
	{#if loading && !items.length && view === 'all'}<p class="muted center">{t('common.loading')}</p>{/if}
	<p class="hint muted">{t('radio.hint')}</p>
</div>

<style>
	.bar {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
		padding: 0 6px 8px;
	}
	.seg {
		display: flex;
		gap: 2px;
		padding: 2px;
		border-radius: 999px;
		background: var(--accent-soft);
	}
	.seg button {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 3px 12px;
		border-radius: 999px;
		font-size: 13px;
	}
	.seg button.on {
		background: var(--surface);
		box-shadow: 0 2px 8px -4px var(--shadow);
	}
	.search {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 4px 12px;
		border-radius: 999px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text-muted);
		flex: 1;
		min-width: 140px;
	}
	.search input {
		border: 0;
		background: none;
		outline: none;
		width: 100%;
		font-size: 14px;
	}
	select {
		max-width: 170px;
		padding: 4px 8px;
		border-radius: 999px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text);
		font-size: 13px;
	}
	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		padding: 0 6px 10px;
	}
	.tags button {
		padding: 2px 10px;
		border-radius: 999px;
		font-size: 12.5px;
		color: var(--text-muted);
		border: 1px dashed var(--line);
	}
	.tags button.on {
		color: var(--text);
		background: var(--accent-soft);
		border-style: solid;
	}
	.https {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		margin-left: auto;
		font-size: 12.5px;
		color: var(--text-muted);
		cursor: pointer;
	}
	.relay-tag {
		display: inline-block;
		margin-right: 6px;
		padding: 0 5px;
		border-radius: 4px;
		border: 1px solid var(--line);
		text-transform: none;
	}
	.stations {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.stations li {
		display: flex;
		align-items: center;
		border-radius: var(--radius-s);
		transition: background var(--dur-fast);
	}
	.stations li:hover {
		background: var(--accent-soft);
	}
	.stations li.cur .title {
		color: var(--accent);
	}
	.row {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 7px 8px;
		text-align: left;
	}
	.logo {
		position: relative;
		width: 38px;
		height: 38px;
		flex: none;
		border-radius: 10px;
		background: var(--accent-soft);
		display: grid;
		place-items: center;
		color: var(--text-muted);
		overflow: hidden;
	}
	.logo img {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: cover;
		background: var(--surface);
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
	.meta {
		font-size: 10.5px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		white-space: nowrap;
	}
	.dot {
		display: inline-block;
		width: 7px;
		height: 7px;
		margin-right: 6px;
		border-radius: 50%;
		background: var(--accent);
		animation: blink 1.6s ease-in-out infinite;
	}
	.fav {
		width: 34px;
		height: 34px;
		color: var(--text-muted);
	}
	.fav.on {
		color: var(--accent);
	}
	.center {
		text-align: center;
		margin: 8vh auto;
	}
	.center-s {
		text-align: center;
	}
	.hint {
		font-size: 11.5px;
		padding: 10px 8px 0;
		margin: 0;
	}
	@media (max-width: 640px) {
		.meta {
			display: none;
		}
	}
	@keyframes blink {
		50% {
			opacity: 0.3;
		}
	}
</style>

