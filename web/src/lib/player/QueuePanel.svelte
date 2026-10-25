<script lang="ts">
	import { coverFallback, coverUrl } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { formatTime } from '$lib/util/format';
	import { player } from './player.svelte';

	let { onclose }: { onclose: () => void } = $props();
	let list: HTMLDivElement | undefined = $state();

	$effect(() => {
		// bring the current item into view when opened
		const el = list?.querySelector('.cur');
		el?.scrollIntoView({ block: 'center' });
	});
</script>

<div class="panel glass" role="dialog" aria-label={t('music.queue')}>
	<header>
		<span class="art">{t('music.queue')} · {t('music.tracks', { n: player.queue.length })}</span>
		<span class="grow"></span>
		{#if player.queue.length}
			<button class="btn small" onclick={() => player.clear()}><Icon name="trash" size={14} />{t('music.clear')}</button>
		{/if}
		<button class="icon-btn" aria-label={t('viewer.close')} onclick={onclose}><Icon name="x" size={18} /></button>
	</header>
	<div class="items" bind:this={list}>
		{#each player.queue as item, i (item.id + ':' + i)}
			<div class="row" class:cur={i === player.index}>
				<button class="main" onclick={() => player.playAt(i)}>
					{#if item.cover}
						<img src={coverUrl(item.cover)} alt="" loading="lazy" use:coverFallback />
					{:else}
						<span class="ph"><Icon name="music" size={14} /></span>
					{/if}
					<span class="txt">
						<span class="title">{item.title}</span>
						<span class="sub">{item.artist || t('music.unknownArtist')}</span>
					</span>
					<span class="dur num">{item.duration_ms ? formatTime(item.duration_ms / 1000) : ''}</span>
				</button>
				<button class="icon-btn rm" aria-label={t('music.remove')} onclick={() => player.remove(i)}>
					<Icon name="x" size={14} />
				</button>
			</div>
		{/each}
	</div>
</div>

<style>
	.panel {
		position: fixed;
		right: 16px;
		bottom: calc(var(--playerbar-h) + 12px);
		z-index: 40;
		width: min(380px, calc(100vw - 32px));
		max-height: min(60vh, 560px);
		display: flex;
		flex-direction: column;
		border-radius: var(--radius-l);
		box-shadow: 0 24px 60px -24px var(--shadow);
		animation: rise var(--dur-base) var(--ease-soft);
	}
	header {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 12px 10px 8px 18px;
		border-bottom: 1px solid var(--line);
	}
	.grow {
		flex: 1;
	}
	.btn.small {
		padding: 0.25em 0.8em;
		font-size: 12px;
	}
	.items {
		overflow-y: auto;
		padding: 6px;
	}
	.row {
		display: flex;
		align-items: center;
		border-radius: 12px;
	}
	.row:hover {
		background: var(--accent-soft);
	}
	.row.cur {
		background: var(--accent-soft);
		color: var(--accent);
	}
	.main {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 6px 8px;
		text-align: left;
	}
	img,
	.ph {
		width: 34px;
		height: 34px;
		border-radius: 8px;
		object-fit: cover;
		flex: none;
	}
	.ph {
		display: grid;
		place-items: center;
		background: var(--accent-soft);
	}
	.txt {
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.title,
	.sub {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.title {
		font-size: 14px;
	}
	.sub {
		font-size: 12px;
		color: var(--text-muted);
	}
	.dur {
		margin-left: auto;
		font-size: 12px;
		color: var(--text-muted);
	}
	.rm {
		width: 30px;
		height: 30px;
		opacity: 0.5;
	}
	.rm:hover {
		opacity: 1;
	}
	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(12px);
		}
	}
	@media (max-width: 640px) {
		.panel {
			left: 8px;
			right: 8px;
			width: auto;
		}
	}
</style>
