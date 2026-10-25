<script lang="ts">
	// Star toggle for a song or station (server-side favourites).
	import type { SavedItem } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { collections } from '$lib/stores/collections.svelte';

	let { item, size = 16 }: { item: () => SavedItem; size?: number } = $props();

	const on = $derived.by(() => {
		const it = item();
		return collections.isFav(it.source, it.id);
	});
</script>

<button
	class="icon-btn fav"
	class:on
	title={on ? t('fav.remove') : t('fav.add')}
	aria-label={on ? t('fav.remove') : t('fav.add')}
	aria-pressed={on}
	onclick={() => collections.toggleFav(item()).catch(() => {})}
>
	<Icon name={on ? 'starFill' : 'star'} {size} />
</button>

<style>
	.fav {
		width: 32px;
		height: 32px;
		color: var(--text-muted);
	}
	.fav.on {
		color: var(--accent);
	}
</style>

