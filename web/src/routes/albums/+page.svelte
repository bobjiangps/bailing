<script lang="ts">
	// The bookshelf. Clicking a book hides it on the shelf and hands its DOM
	// element to <Book>, which flies a copy of it to the centre and opens it.
	import { api, type Album } from '$lib/api/client';
	import BookCover from '$lib/gallery/BookCover.svelte';
	import Book from '$lib/gallery/Book.svelte';
	import { t } from '$lib/i18n';
	import { requireLogin, session } from '$lib/stores/session.svelte';
	import { features } from '$lib/features.svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';

	let albums = $state<Album[]>([]);
	let scanning = $state(false);
	let loaded = $state(false);
	let failed = $state(false);
	let open = $state<{ album: Album; el: HTMLElement } | null>(null);

	$effect(requireLogin);

	// this edition / server does not include this half: back to the home page
	$effect(() => {
		if (session.ready && !features.photos) goto(base + '/', { replaceState: true });
	});

	async function load() {
		failed = false;
		try {
			const r = await api<{ albums: Album[]; scanning: boolean }>('/api/albums');
			albums = r.albums;
			scanning = r.scanning;
		} catch {
			failed = true;
		}
		loaded = true;
	}

	$effect(() => {
		if (!session.user) return;
		load();
	});

	// while the server is still scanning, refresh quietly every few seconds
	$effect(() => {
		if (!scanning) return;
		const id = setInterval(load, 5000);
		return () => clearInterval(id);
	});
</script>

<svelte:head><title>{t('nav.albums')} · {t('brand')}</title></svelte:head>

<div class="page">
	<header class="head">
		<h1 class="art">{t('albums.title')}</h1>
		{#if scanning}<span class="chip">{t('albums.scanning')}</span>{/if}
	</header>

	{#if failed}
		<p class="muted center">{t('common.error')} <button class="btn" onclick={load}>{t('common.retry')}</button></p>
	{:else if loaded && !albums.length}
		<p class="muted center empty">{t('albums.empty')}</p>
	{:else}
		<div class="shelf">
			{#each albums as album, i (album.id)}
				<BookCover
					{album}
					index={i}
					hidden={open?.album.id === album.id}
					onopen={(a, el) => (open = { album: a, el })}
				/>
			{/each}
		</div>
	{/if}
</div>

{#if open}
	<Book album={open.album} origin={open.el} onclose={() => (open = null)} />
{/if}

<style>
	.head {
		display: flex;
		align-items: baseline;
		gap: 14px;
		margin-bottom: 28px;
	}
	h1 {
		margin: 0;
		font-weight: 400;
		font-size: clamp(26px, 4vw, 38px);
	}
	.shelf {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 46px 30px;
		padding: 8px 4px 24px;
	}
	@media (max-width: 520px) {
		.shelf {
			grid-template-columns: repeat(2, minmax(0, 1fr));
			gap: 34px 18px;
		}
	}
	.center {
		text-align: center;
		margin-top: 12vh;
	}
	.empty {
		max-width: 420px;
		margin-inline: auto;
		line-height: 1.9;
	}
</style>


