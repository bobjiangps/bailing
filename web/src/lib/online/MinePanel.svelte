<script lang="ts">
	// "Mine": favourite songs, favourite stations and playlists. Everything
	// is stored on the server, so it is the same on every device.
	import { coverFallback, coverUrl, type Playlist, type SavedItem } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { fromSaved, player } from '$lib/player/player.svelte';
	import { collections, toSaved } from '$lib/stores/collections.svelte';
	import { formatTime } from '$lib/util/format';
	import FavButton from './FavButton.svelte';

	type View = 'songs' | 'stations' | 'lists';
	let view = $state<View>('songs');
	let open = $state<Playlist | null>(null);
	let editing = $state(false);
	let rename = $state('');
	let newName = $state('');
	let busy = $state(false);
	let err = $state('');

	const list = $derived<SavedItem[]>(
		open ? open.items : view === 'songs' ? collections.tracks : view === 'stations' ? collections.stations : []
	);

	$effect(() => {
		if (!collections.loaded) collections.load();
	});

	function play(i: number) {
		player.playList(list.map(fromSaved), i);
	}

	async function openList(id: number) {
		err = '';
		try {
			open = await collections.get(id);
			rename = open.name;
			editing = false;
		} catch {
			err = t('common.error');
		}
	}

	async function persist(items: SavedItem[]) {
		if (!open) return;
		const before = open.items;
		open.items = items;
		try {
			await collections.save(open.id, { items });
		} catch {
			open.items = before;
			err = t('common.error');
		}
	}

	function move(i: number, d: number) {
		if (!open) return;
		const j = i + d;
		if (j < 0 || j >= open.items.length) return;
		const items = [...open.items];
		[items[i], items[j]] = [items[j], items[i]];
		persist(items);
	}

	function drop(i: number) {
		if (open) persist(open.items.filter((_, k) => k !== i));
	}

	async function saveName() {
		if (!open || !rename.trim()) return;
		await collections.save(open.id, { name: rename.trim() }).catch(() => (err = t('common.error')));
		open.name = rename.trim();
		editing = false;
	}

	async function del() {
		if (!open || !confirm(t('pl.confirmDelete', { name: open.name }))) return;
		await collections.remove(open.id).catch(() => (err = t('common.error')));
		open = null;
	}

	async function create(fromQueue: boolean) {
		const n = newName.trim() || (fromQueue ? t('pl.queueName') : '');
		if (!n) return;
		busy = true;
		try {
			const items = fromQueue ? player.queue.map((q) => toSaved(q)) : [];
			const id = await collections.create(n, items);
			newName = '';
			await openList(id);
		} catch {
			err = t('common.error');
		} finally {
			busy = false;
		}
	}

	const sub = (s: SavedItem) =>
		s.source === 'radio' ? s.artist : `${s.artist || t('music.unknownArtist')}${s.album ? ' · ' + s.album : ''}`;
	const srcTag = (s: SavedItem) =>
		s.source === 'local' ? '' : s.source === 'radio' ? t('music.radio') : s.source.startsWith('lx:') ? s.source.slice(3) : t('music.online');
</script>

<div class="mine">
	{#if open}
		<div class="crumb">
			<button class="icon-btn" aria-label={t('music.back')} onclick={() => (open = null)}><Icon name="back" size={18} /></button>
			{#if editing}
				<form class="rename" onsubmit={(e) => (e.preventDefault(), saveName())}>
					<input bind:value={rename} maxlength="80" aria-label={t('pl.rename')} />
					<button class="btn" type="submit">{t('common.save')}</button>
				</form>
			{:else}
				<span class="art name">{open.name}</span>
				<button class="icon-btn" title={t('pl.rename')} aria-label={t('pl.rename')} onclick={() => (editing = true)}><Icon name="text" size={15} /></button>
				<button class="icon-btn" title={t('pl.delete')} aria-label={t('pl.delete')} onclick={del}><Icon name="trash" size={15} /></button>
			{/if}
		</div>
	{:else}
		<div class="seg">
			<button class:on={view === 'songs'} onclick={() => (view = 'songs')}>{t('mine.songs')} · {collections.tracks.length}</button>
			<button class:on={view === 'stations'} onclick={() => (view = 'stations')}>{t('mine.stations')} · {collections.stations.length}</button>
			<button class:on={view === 'lists'} onclick={() => (view = 'lists')}>{t('mine.lists')} · {collections.playlists.length}</button>
		</div>
	{/if}

	{#if err}<p class="err">{err}</p>{/if}

	{#if !open && view === 'lists'}
		<form class="new" onsubmit={(e) => (e.preventDefault(), create(false))}>
			<input bind:value={newName} maxlength="80" placeholder={t('pl.newName')} aria-label={t('pl.newName')} />
			<button class="btn" type="submit" disabled={busy || !newName.trim()}>{t('pl.create')}</button>
			<button class="btn" type="button" disabled={busy || !player.queue.length} onclick={() => create(true)}>{t('pl.fromQueue')}</button>
		</form>
		{#if !collections.playlists.length}
			<p class="muted center art">{t('pl.empty')}</p>
		{:else}
			<ul class="lists">
				{#each collections.playlists as p (p.id)}
					<li>
						<button onclick={() => openList(p.id)}>
							{#if p.first?.cover}<img src={coverUrl(p.first.cover)} alt="" loading="lazy" use:coverFallback />{:else}<span class="ph"><Icon name="music" size={22} /></span>{/if}
							<span class="nm">{p.name}</span>
							<span class="muted num">{t('music.tracks', { n: p.count })}</span>
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	{:else if !list.length}
		<p class="muted center art">{open ? t('pl.itemsEmpty') : view === 'songs' ? t('mine.songsEmpty') : t('radio.favEmpty')}</p>
	{:else}
		<div class="list-head">
			<span class="muted num">{t('music.tracks', { n: list.length })}</span>
			<button class="btn primary" onclick={() => play(0)}><Icon name="play" size={14} />{t('music.playAll')}</button>
		</div>
		<ol class="rows">
			{#each list as it, i (it.source + '|' + it.id + '|' + i)}
				{@const isCur = player.current?.source === it.source && player.current.id === it.id}
				<li class:cur={isCur}>
					<button class="row" onclick={() => play(i)}>
						{#if it.cover}<img src={coverUrl(it.cover)} alt="" loading="lazy" use:coverFallback />{:else}<span class="ph"><Icon name={it.source === 'radio' ? 'radio' : 'disc'} size={16} /></span>{/if}
						<span class="tt">
							<span class="title">{it.title}</span>
							<span class="sub muted">{sub(it)}</span>
						</span>
						{#if srcTag(it)}<span class="src muted">{srcTag(it)}</span>{/if}
						<span class="dur num muted">{it.duration_ms ? formatTime(it.duration_ms / 1000) : ''}</span>
					</button>
					{#if open}
						<button class="icon-btn" aria-label={t('pl.up')} disabled={i === 0} onclick={() => move(i, -1)}><Icon name="up" size={14} /></button>
						<button class="icon-btn" aria-label={t('pl.remove')} title={t('pl.remove')} onclick={() => drop(i)}><Icon name="x" size={14} /></button>
					{:else}
						<FavButton item={() => it} size={15} />
					{/if}
				</li>
			{/each}
		</ol>
	{/if}
	<p class="hint muted">{t('mine.hint')}</p>
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
		padding: 3px 12px;
		border-radius: 999px;
		font-size: 13px;
	}
	.seg button.on {
		background: var(--surface);
		box-shadow: 0 2px 8px -4px var(--shadow);
	}
	.crumb {
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 0 4px 8px;
	}
	.crumb .name {
		font-size: 17px;
		margin-right: 4px;
	}
	.rename,
	.new {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}
	.new {
		padding: 0 6px 12px;
	}
	.rename input,
	.new input {
		flex: 1;
		min-width: 140px;
		padding: 4px 10px;
		border-radius: 999px;
		border: 1px solid var(--line);
		background: var(--surface);
		font-size: 14px;
	}
	.new .btn,
	.rename .btn {
		font-size: 13px;
		padding: 0.35em 1em;
	}
	.lists {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
		gap: 10px;
	}
	.lists button {
		width: 100%;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 4px;
		padding: 10px;
		border-radius: var(--radius-m);
		text-align: left;
	}
	.lists button:hover {
		background: var(--accent-soft);
	}
	.lists img,
	.lists .ph {
		width: 100%;
		aspect-ratio: 1;
		border-radius: var(--radius-s);
		object-fit: cover;
		background: var(--accent-soft);
		display: grid;
		place-items: center;
		color: var(--text-muted);
	}
	.nm {
		font-size: 14px;
		max-width: 100%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.lists .muted {
		font-size: 12px;
	}
	.list-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 0 6px 8px;
	}
	.list-head .btn {
		font-size: 13px;
		padding: 0.4em 1em;
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.rows li {
		display: flex;
		align-items: center;
		border-radius: var(--radius-s);
	}
	.rows li:hover {
		background: var(--accent-soft);
	}
	.rows li.cur .title {
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
	.row img,
	.row .ph {
		width: 36px;
		height: 36px;
		flex: none;
		border-radius: 8px;
		object-fit: cover;
		background: var(--accent-soft);
		display: grid;
		place-items: center;
		color: var(--text-muted);
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
	.src {
		font-size: 10.5px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.dur {
		font-size: 12.5px;
		width: 44px;
		text-align: right;
	}
	.rows .icon-btn {
		width: 30px;
		height: 30px;
	}
	.center {
		text-align: center;
		margin: 8vh auto;
	}
	.err {
		color: var(--accent);
		padding: 0 8px;
	}
	.hint {
		font-size: 11.5px;
		padding: 10px 8px 0;
		margin: 0;
	}
	@media (max-width: 640px) {
		.src {
			display: none;
		}
	}
</style>



