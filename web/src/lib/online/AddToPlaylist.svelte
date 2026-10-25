<script lang="ts">
	// "Add to playlist" button with a small menu: pick an existing playlist
	// or create a new one. Used on track rows, stations, search results and
	// the player bar.
	import type { SavedItem } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { collections } from '$lib/stores/collections.svelte';

	let {
		items,
		size = 16,
		label = ''
	}: { items: () => SavedItem[]; size?: number; label?: string } = $props();

	let open = $state(false);
	let name = $state('');
	let msg = $state('');
	let busy = $state(false);
	let root: HTMLElement | undefined = $state();
	let up = $state(false);

	function toggle() {
		open = !open;
		msg = '';
		name = '';
		if (open && root) up = root.getBoundingClientRect().top > innerHeight * 0.55;
	}

	async function addTo(id: number) {
		busy = true;
		try {
			const r = await collections.add(id, items());
			msg = r.added ? t('pl.added', { n: r.added }) : t('pl.already');
			setTimeout(() => (open = false), 900);
		} catch {
			msg = t('common.error');
		} finally {
			busy = false;
		}
	}

	async function createWith() {
		const n = name.trim();
		if (!n) return;
		busy = true;
		try {
			await collections.create(n, items());
			msg = t('pl.created', { name: n });
			setTimeout(() => (open = false), 900);
		} catch {
			msg = t('common.error');
		} finally {
			busy = false;
		}
	}

	function onDoc(e: MouseEvent) {
		if (open && root && !root.contains(e.target as Node)) open = false;
	}
</script>

<svelte:document onclick={onDoc} onkeydown={(e) => e.key === 'Escape' && (open = false)} />

<span class="atp" bind:this={root}>
	<button class="icon-btn" class:labelled={!!label} title={t('pl.addTo')} aria-label={t('pl.addTo')} aria-expanded={open} onclick={toggle}>
		<Icon name="listPlus" {size} />{#if label}<span>{label}</span>{/if}
	</button>
	{#if open}
		<div class="menu glass" class:up role="menu">
			<p class="head muted">{t('pl.addTo')}</p>
			{#if collections.playlists.length}
				<ul>
					{#each collections.playlists as p (p.id)}
						<li>
							<button role="menuitem" disabled={busy} onclick={() => addTo(p.id)}>
								<span class="nm">{p.name}</span><span class="muted num">{p.count}</span>
							</button>
						</li>
					{/each}
				</ul>
			{/if}
			<form class="new" onsubmit={(e) => (e.preventDefault(), createWith())}>
				<input bind:value={name} maxlength="80" placeholder={t('pl.newName')} aria-label={t('pl.newName')} />
				<button class="btn" type="submit" disabled={busy || !name.trim()}>{t('pl.create')}</button>
			</form>
			{#if msg}<p class="msg">{msg}</p>{/if}
		</div>
	{/if}
</span>

<style>
	.atp {
		position: relative;
		display: inline-flex;
	}
	.icon-btn {
		width: 32px;
		height: 32px;
		color: var(--text-muted);
	}
	.icon-btn.labelled {
		width: auto;
		padding: 0 10px;
		gap: 6px;
		font-size: 13px;
	}
	.menu {
		position: absolute;
		right: 0;
		top: calc(100% + 6px);
		z-index: 60;
		width: 240px;
		padding: 8px;
		border-radius: var(--radius-m);
		box-shadow: 0 20px 40px -20px var(--shadow);
		text-align: left;
	}
	.menu.up {
		top: auto;
		bottom: calc(100% + 6px);
	}
	.head {
		margin: 2px 6px 6px;
		font-size: 12px;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		max-height: 220px;
		overflow: auto;
	}
	li button {
		width: 100%;
		display: flex;
		justify-content: space-between;
		gap: 8px;
		padding: 6px 8px;
		border-radius: var(--radius-s);
		font-size: 14px;
	}
	li button:hover {
		background: var(--accent-soft);
	}
	.nm {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.new {
		display: flex;
		gap: 6px;
		margin-top: 6px;
	}
	.new input {
		flex: 1;
		min-width: 0;
		padding: 4px 8px;
		border-radius: var(--radius-s);
		border: 1px solid var(--line);
		background: var(--surface);
		font-size: 13px;
	}
	.new .btn {
		font-size: 13px;
		padding: 0.3em 0.8em;
	}
	.msg {
		margin: 6px 6px 0;
		font-size: 12.5px;
		color: var(--accent);
	}
</style>

