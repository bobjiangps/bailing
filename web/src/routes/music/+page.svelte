<script lang="ts">
	// Music: the turntable + lyrics on one side, the library on the other.
	// Phone: one column, with a small switch between "record" and "lyrics".
	import { api, coverUrl, type Group, type Track } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import OnlinePanel from '$lib/online/OnlinePanel.svelte';
	import RadioPanel from '$lib/online/RadioPanel.svelte';
	import MinePanel from '$lib/online/MinePanel.svelte';
	import FavButton from '$lib/online/FavButton.svelte';
	import AddToPlaylist from '$lib/online/AddToPlaylist.svelte';
	import DownloadButton from '$lib/online/DownloadButton.svelte';
	import { toSaved } from '$lib/stores/collections.svelte';
	import Lyrics from '$lib/player/Lyrics.svelte';
	import TrafficChip from '$lib/player/TrafficChip.svelte';
	import Vinyl from '$lib/player/Vinyl.svelte';
	import Spectrum from '$lib/player/Spectrum.svelte';
	import { fromTrack, player } from '$lib/player/player.svelte';
	import { requireLogin, session } from '$lib/stores/session.svelte';
	import { features } from '$lib/features.svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { formatTime } from '$lib/util/format';

	type LibTab = 'all' | 'folders' | 'artists' | 'albums';
	type Tab = LibTab | 'mine' | 'radio' | 'online';
	const TABS: Tab[] = ['all', 'folders', 'artists', 'albums', 'mine', 'radio', 'online'];
	const WIDE_MIN = 1100;
	const FILTER: Record<Exclude<LibTab, 'all'>, 'folder' | 'artist' | 'album'> = {
		folders: 'folder',
		artists: 'artist',
		albums: 'album'
	};

	let tab = $state<Tab>(initialTab());
	let q = $state('');
	let group = $state<string | null>(null); // selected folder / artist / album
	let tracks = $state<Track[]>([]);
	let groups = $state<Group[]>([]);
	let total = $state(0);
	let scanning = $state(false);
	let loading = $state(false);
	let failed = $state(false);
	let face = $state<'disc' | 'lyrics'>('disc');
	let vw = $state(1200);
	// wide screens: record left, lyrics right, library in a drawer that can be hidden
	const wide = $derived(vw >= WIDE_MIN);
	let libOpen = $state(readLibOpen());

	const vinylSize = $derived(vw < 640 ? Math.min(vw - 96, 300) : vw < 1024 ? 280 : 340);
	const isLib = $derived(tab !== 'radio' && tab !== 'online' && tab !== 'mine');
	const showGroups = $derived(isLib && tab !== 'all' && group === null && !q);

	function readLibOpen(): boolean {
		try {
			return localStorage.getItem('bl.libOpen') !== '0';
		} catch {
			return true;
		}
	}

	function setLibOpen(v: boolean) {
		libOpen = v;
		try {
			localStorage.setItem('bl.libOpen', v ? '1' : '0');
		} catch {
			/* ignore */
		}
	}

	/*
	 * Auto mode (default on): the drawer covers the lyrics, so it slides away
	 * when playback starts and comes back on pause. Only the play/pause
	 * transition moves it; in between the user can open / close it freely.
	 * Short delays: a double-click in the list is still seen, and the gap
	 * between two songs (playing briefly false) does not flash the drawer.
	 */
	let libAuto = $state(readLibAuto());

	function readLibAuto(): boolean {
		try {
			return localStorage.getItem('bl.libAuto') !== '0';
		} catch {
			return true;
		}
	}

	function setLibAuto(v: boolean) {
		libAuto = v;
		try {
			localStorage.setItem('bl.libAuto', v ? '1' : '0');
		} catch {
			/* ignore */
		}
	}

	$effect(() => {
		if (!wide || !libAuto) return;
		const playing = player.playing;
		const h = setTimeout(() => (libOpen = !playing), playing ? 700 : 900);
		return () => clearTimeout(h);
	});

	function onKey(e: KeyboardEvent) {
		const el = e.target as HTMLElement;
		if (!wide || e.ctrlKey || e.metaKey || e.altKey) return;
		if (el?.tagName === 'INPUT' || el?.tagName === 'TEXTAREA' || el?.tagName === 'SELECT' || el?.isContentEditable) return;
		if (e.key === 'l' || e.key === 'L') setLibOpen(!libOpen);
		else if (e.key === 'Escape' && libOpen && !document.querySelector('.fx-panel, .menu')) setLibOpen(false);
	}

	function initialTab(): Tab {
		try {
			const v = sessionStorage.getItem('bl.musicTab') as Tab | null;
			return v && TABS.includes(v) ? v : 'all';
		} catch {
			return 'all';
		}
	}

	$effect(requireLogin);

	// this edition / server does not include this half: back to the home page
	$effect(() => {
		if (session.ready && !features.music) goto(base + '/', { replaceState: true });
	});

	let reqId = 0;
	async function load() {
		if (!isLib) return;
		const my = ++reqId;
		loading = true;
		failed = false;
		try {
			if (showGroups) {
				const r = await api<{ groups: Group[] }>(`/api/music/groups/${tab}`);
				if (my === reqId) groups = r.groups;
			} else {
				const p = new URLSearchParams({ limit: '5000' });
				if (q) p.set('q', q);
				if (tab !== 'all' && group !== null) p.set(FILTER[tab as Exclude<LibTab, 'all'>], group);
				const r = await api<{ tracks: Track[]; total: number; scanning: boolean }>(`/api/music/tracks?${p}`);
				if (my === reqId) {
					tracks = r.tracks;
					total = r.total;
					scanning = r.scanning;
				}
			}
		} catch {
			if (my === reqId) failed = true;
		} finally {
			if (my === reqId) loading = false;
		}
	}

	// reload on tab / group / search change (search is debounced)
	$effect(() => {
		if (!session.user) return;
		void tab;
		void group;
		void showGroups;
		const handle = setTimeout(load, q ? 250 : 0);
		return () => clearTimeout(handle);
	});

	$effect(() => {
		if (!scanning) return;
		const id = setInterval(load, 6000);
		return () => clearInterval(id);
	});

	function pickTab(tb: Tab) {
		tab = tb;
		group = null;
		try {
			sessionStorage.setItem('bl.musicTab', tb);
		} catch {
			/* ignore */
		}
	}

	function playFrom(i: number) {
		player.playList(tracks.map(fromTrack), i);
	}

	// "" is a real group (no tag / files at the top of the music folder)
	const nameOf = (name: string, kind: Tab) =>
		name && name !== '.'
			? name
			: kind === 'folders'
				? t('music.rootFolder')
				: kind === 'albums'
					? t('music.unknownAlbum')
					: t('music.unknownArtist');
	const groupLabel = (g: Group) => nameOf(g.name, tab);
</script>

<svelte:window bind:innerWidth={vw} onkeydown={onKey} />
<svelte:head>
	<title>{player.current ? `${player.current.title} · ` : ''}{t('nav.music')} · {t('brand')}</title>
</svelte:head>

<div class="page music" class:wide class:lib-open={wide && libOpen}>
	<!-- ===== now playing ===== -->
	<section class="stage">
		<div class="switch" role="tablist">
			<button role="tab" aria-selected={face === 'disc'} class:on={face === 'disc'} onclick={() => (face = 'disc')}>
				{t('music.disc')}
			</button>
			<button role="tab" aria-selected={face === 'lyrics'} class:on={face === 'lyrics'} onclick={() => (face = 'lyrics')}>
				{t('music.lyricsTab')}
			</button>
		</div>

		<div class="disc-side" class:hide-m={face !== 'disc'}>
			<Vinyl size={vinylSize} />
			<Spectrum width={Math.min(vinylSize + 40, 380)} />
			<div class="now">
				{#if player.current}
					<h2 class="art">{player.current.title}</h2>
					<p class="muted">
						{player.current.artist || t('music.unknownArtist')}{#if player.current.album} · {player.current.album}{/if}
					</p>
					<div class="now-acts">
						<TrafficChip />
						{#if player.current}
							{@const cur = player.current}
							<DownloadButton item={() => cur} size={16} />
						{/if}
					</div>
				{:else}
					<p class="muted art">{t('music.nothing')}</p>
				{/if}
			</div>
		</div>

		{#if !wide}
			<div class="lyrics-side" class:hide-m={face !== 'lyrics'}>
				<Lyrics />
			</div>
		{/if}
	</section>

	{#if wide}
		<section class="lyrics-col" aria-label={t('music.lyricsTab')}>
			<Lyrics />
		</section>
		{#if !libOpen}
			<button class="lib-toggle glass" title={t('music.libKeys')} onclick={() => setLibOpen(true)}>
				<Icon name="library" size={16} />{t('music.library')}<kbd>L</kbd>
			</button>
		{/if}
	{/if}

	<!-- ===== library (a drawer on wide screens) ===== -->
	<section class="library glass" class:drawer={wide} class:open={libOpen} inert={wide && !libOpen}>
		{#if wide}
			<div class="drawer-bar">
				<p class="muted small">{t('music.libHint')}</p>
				<label class="auto small">
					<input type="checkbox" checked={libAuto} onchange={(e) => setLibAuto(e.currentTarget.checked)} />
					{t('music.libAuto')}
				</label>
				<button class="btn hide-btn" title={t('music.libKeys')} onclick={() => setLibOpen(false)}>
					<Icon name="x" size={14} />{t('music.hideLibrary')}<kbd>Esc</kbd>
				</button>
			</div>
		{/if}
		<div class="lib-head">
			<div class="tabs">
				{#each TABS as tb (tb)}
					<button class:on={tab === tb} class:ext={tb === 'radio' || tb === 'online' || tb === 'mine'} onclick={() => pickTab(tb)}>
						{#if tb === 'radio'}<Icon name="radio" size={14} />{:else if tb === 'online'}<Icon name="globe" size={14} />{:else if tb === 'mine'}<Icon name="star" size={14} />{/if}
						{t('music.' + tb)}
					</button>
				{/each}
			</div>
			{#if isLib}
				<label class="search">
					<Icon name="search" size={16} />
					<input type="search" bind:value={q} placeholder={t('music.search')} aria-label={t('music.search')} />
				</label>
			{/if}
		</div>

		{#if tab === 'mine'}
			<MinePanel />
		{:else if tab === 'radio'}
			<RadioPanel />
		{:else if tab === 'online'}
			<OnlinePanel />
		{:else}
		{#if group !== null}
			<div class="crumb">
				<button class="icon-btn" aria-label={t('music.back')} onclick={() => (group = null)}>
					<Icon name="back" size={18} />
				</button>
				<span class="art">{nameOf(group, tab)}</span>
			</div>
		{/if}

		{#if scanning}<p class="chip scan">{t('music.scanning')}</p>{/if}

		{#if failed}
			<p class="muted center">{t('common.error')} <button class="btn" onclick={load}>{t('common.retry')}</button></p>
		{:else if showGroups}
			<ul class="groups">
				{#each groups as g (g.name)}
					<li>
						<button onclick={() => (group = g.name)}>
							{#if g.cover}
								<img src={coverUrl(g.cover)} alt="" loading="lazy" />
							{:else}
								<span class="ph"><Icon name={tab === 'folders' ? 'folder' : 'disc'} size={20} /></span>
							{/if}
							<span class="g-name">{groupLabel(g)}</span>
							<span class="muted num">{t('music.tracks', { n: g.count })}</span>
						</button>
					</li>
				{/each}
			</ul>
		{:else if !loading && !tracks.length}
			<p class="muted center empty">{q ? '—' : t('music.empty')}</p>
		{:else}
			<div class="list-head">
				<span class="muted num">{t('music.tracks', { n: total })}</span>
				<span class="head-acts">
					<AddToPlaylist items={() => tracks.map((x) => toSaved(x))} size={15} label={t('pl.addAll')} />
					<button class="btn primary" disabled={!tracks.length} onclick={() => playFrom(0)}>
						<Icon name="play" size={14} />{t('music.playAll')}
					</button>
				</span>
			</div>
			<ol class="tracks">
				{#each tracks as tr, i (tr.id)}
					{@const isCur = player.current?.source === 'local' && player.current.id === tr.id}
					<li class:cur={isCur}>
						<button class="row" ondblclick={() => playFrom(i)} onclick={(e) => e.detail === 1 && vw < 900 && playFrom(i)}>
							<span class="idx num muted">
								{#if isCur && player.playing}<span class="eq"><i></i><i></i><i></i></span>{:else}{i + 1}{/if}
							</span>
							<span class="tt">
								<span class="title">{tr.title}</span>
								<span class="sub muted">{tr.artist || t('music.unknownArtist')}{#if tr.album} · {tr.album}{/if}</span>
							</span>
							<span class="fmt muted">{tr.format}</span>
							<span class="dur num muted">{tr.duration_ms ? formatTime(tr.duration_ms / 1000) : ''}</span>
						</button>
						<span class="acts">
							<button class="icon-btn" title={t('player.play')} onclick={() => playFrom(i)}>
								<Icon name="play" size={15} />
							</button>
							<FavButton item={() => toSaved(tr)} size={15} />
							<AddToPlaylist items={() => [toSaved(tr)]} size={15} />
							<DownloadButton item={() => fromTrack(tr)} size={15} />
							<button class="icon-btn" title={t('music.playNext')} onclick={() => player.playNext(fromTrack(tr))}>
								<Icon name="queue" size={15} />
							</button>
						</span>
					</li>
				{/each}
			</ol>
		{/if}
		{/if}
	</section>
</div>

<style>
	.music {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1.05fr);
		gap: 32px;
		align-items: start;
		max-width: 1280px;
	}
	@media (max-width: 960px) {
		.music {
			grid-template-columns: 1fr;
		}
	}

	/* ---- wide screens: record | lyrics, library in a drawer ---- */
	.music.wide {
		max-width: 1440px;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
		gap: 40px;
		/* both columns fill the space between the top bar and the player bar */
		--col-h: calc(100dvh - var(--topbar-h) - var(--playerbar-h) - 64px);
	}
	.music.wide .stage {
		min-height: var(--col-h);
		justify-content: center;
		transition: transform var(--dur-base) var(--ease-soft);
	}
	.lyrics-col {
		position: sticky;
		top: calc(var(--topbar-h) + 16px);
		height: var(--col-h);
		display: flex;
		flex-direction: column;
		transition: opacity var(--dur-base) var(--ease-soft);
	}
	.music.wide.lib-open .lyrics-col {
		opacity: 0.25;
	}
	.lib-toggle {
		position: fixed;
		top: calc(var(--topbar-h) + 12px);
		right: 24px;
		z-index: 20;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 7px 16px;
		border-radius: 999px;
		font-size: 14px;
		box-shadow: 0 10px 30px -18px var(--shadow);
	}
	.library.drawer {
		position: fixed;
		z-index: 20;
		top: calc(var(--topbar-h) + 8px);
		right: 16px;
		bottom: calc(var(--playerbar-h) + 12px);
		width: min(600px, 48vw);
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		box-shadow: 0 24px 60px -30px var(--shadow);
		transform: translateX(calc(100% + 24px));
		visibility: hidden;
		transition:
			transform var(--dur-base) var(--ease-soft),
			visibility 0s linear var(--dur-base);
	}
	.library.drawer.open {
		transform: none;
		visibility: visible;
		transition: transform var(--dur-base) var(--ease-soft);
	}
	.drawer-bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 12px;
		margin: 0 0 12px;
		padding-bottom: 10px;
		border-bottom: 1px dashed var(--line);
	}
	.drawer-bar p {
		flex: 1 1 100%;
		margin: 0;
		line-height: 1.6;
	}
	.drawer-bar .auto {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		cursor: pointer;
	}
	.drawer-bar .hide-btn {
		margin-left: auto;
		padding: 4px 12px;
		font-size: 13px;
	}
	.small {
		font-size: 12.5px;
	}
	kbd {
		margin-left: 6px;
		padding: 0 5px;
		border: 1px solid var(--line);
		border-radius: 4px;
		font: 11px/1.6 var(--font-mono, ui-monospace, monospace);
		color: var(--text-muted);
	}
	@media (prefers-reduced-motion: reduce) {
		.library.drawer,
		.library.drawer.open {
			transition: none;
		}
	}

	/* ---- stage ---- */
	.stage {
		position: sticky;
		top: calc(var(--topbar-h) + 16px);
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 20px;
	}
	@media (max-width: 960px) {
		.stage {
			position: static;
		}
	}
	.switch {
		display: none;
		gap: 4px;
		padding: 3px;
		border-radius: 999px;
		background: var(--accent-soft);
	}
	.switch button {
		padding: 4px 16px;
		border-radius: 999px;
		font-size: 13px;
	}
	.switch button.on {
		background: var(--surface);
		box-shadow: 0 2px 8px -4px var(--shadow);
	}
	.disc-side {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 22px;
	}
	.now {
		text-align: center;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		max-width: 420px;
	}
	.now h2 {
		margin: 0;
		font-weight: 400;
		font-size: 24px;
		line-height: 1.3;
	}
	.now p {
		margin: 0;
	}
	.now-acts {
		display: flex;
		align-items: center;
		gap: 4px;
	}
	.lyrics-side {
		width: 100%;
		height: 260px;
		display: flex;
		flex-direction: column;
	}
	@media (max-width: 960px) {
		.switch {
			display: flex;
		}
		.hide-m {
			display: none;
		}
		.lyrics-side {
			height: min(60dvh, 460px);
		}
	}

	/* ---- library ---- */
	.library {
		border-radius: var(--radius-l);
		padding: 18px 14px 14px;
		min-height: 60dvh;
	}
	.lib-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 0 6px 12px;
	}
	.tabs {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
	}
	.tabs button {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 4px 12px;
		border-radius: 999px;
		font-size: 14px;
		color: var(--text-muted);
		transition: background var(--dur-fast), color var(--dur-fast);
	}
	.tabs button.on {
		background: var(--accent-soft);
		color: var(--text);
	}
	.search {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 5px 12px;
		border-radius: 999px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text-muted);
		flex: 1;
		max-width: 260px;
		min-width: 160px;
	}
	.search input {
		border: 0;
		background: none;
		outline: none;
		width: 100%;
		font-size: 14px;
	}
	.crumb {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 0 4px 8px;
		font-size: 17px;
	}
	.scan {
		margin: 0 6px 8px;
	}
	.center {
		text-align: center;
		margin: 10vh auto;
	}
	.empty {
		max-width: 380px;
		line-height: 1.9;
	}

	.groups {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 10px;
	}
	.groups button {
		width: 100%;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 4px;
		padding: 10px;
		border-radius: var(--radius-m);
		text-align: left;
		transition: background var(--dur-fast);
	}
	.groups button:hover {
		background: var(--accent-soft);
	}
	.groups img,
	.groups .ph {
		width: 100%;
		aspect-ratio: 1;
		border-radius: var(--radius-s);
		object-fit: cover;
		background: var(--accent-soft);
		display: grid;
		place-items: center;
		color: var(--text-muted);
	}
	.g-name {
		font-size: 14px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		max-width: 100%;
	}
	.groups .muted {
		font-size: 12px;
	}

	.list-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 0 6px 8px;
	}
	.head-acts {
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.list-head .btn {
		font-size: 13px;
		padding: 0.4em 1em;
	}
	.tracks {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.tracks li {
		position: relative;
		display: flex;
		align-items: center;
		border-radius: var(--radius-s);
		transition: background var(--dur-fast);
		content-visibility: auto;
		contain-intrinsic-size: auto 52px;
	}
	.tracks li:hover {
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
		gap: 12px;
		padding: 7px 8px;
		text-align: left;
	}
	.idx {
		width: 28px;
		text-align: right;
		font-size: 12.5px;
		flex: none;
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
		font-size: 12.5px;
	}
	.fmt {
		font-size: 10.5px;
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}
	.dur {
		font-size: 12.5px;
		width: 44px;
		text-align: right;
	}
	.acts {
		display: flex;
		/* faint until hover, so the actions (incl. download) are discoverable */
		opacity: 0.4;
		transition: opacity var(--dur-fast);
	}
	.tracks li:hover .acts,
	.tracks li:focus-within .acts {
		opacity: 1;
	}
	.acts .icon-btn {
		width: 32px;
		height: 32px;
	}
	@media (max-width: 640px) {
		.fmt {
			display: none;
		}
		.acts {
			opacity: 1;
		}
		.acts .icon-btn:first-child {
			display: none;
		}
	}

	/* tiny equaliser for the playing row */
	.eq {
		display: inline-flex;
		gap: 2px;
		align-items: flex-end;
		height: 12px;
	}
	.eq i {
		width: 3px;
		background: var(--accent);
		border-radius: 1px;
		animation: eq 900ms ease-in-out infinite;
	}
	.eq i:nth-child(2) {
		animation-delay: -300ms;
	}
	.eq i:nth-child(3) {
		animation-delay: -600ms;
	}
	@keyframes eq {
		0%,
		100% {
			height: 3px;
		}
		50% {
			height: 12px;
		}
	}
</style>

