<script lang="ts">
	// Saves a song to this device (see player/download.ts).
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { canDownload, download } from '$lib/player/download';
	import type { QueueItem } from '$lib/player/player.svelte';

	let { item, size = 16 }: { item: () => QueueItem; size?: number } = $props();

	let busy = $state(false);
	let err = $state('');
	let note = $state('');
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function go() {
		if (busy) return;
		busy = true;
		err = '';
		note = '';
		clearTimeout(timer);
		try {
			const r = await download(item());
			if (r.lower) {
				note = t('music.dlQuality', { q: r.lower });
				timer = setTimeout(() => (note = ''), 6000);
			}
		} catch (e) {
			err = t('music.downloadFail', { e: e instanceof Error ? e.message : String(e) });
			timer = setTimeout(() => (err = ''), 6000);
		} finally {
			busy = false;
		}
	}
</script>

{#if canDownload(item())}
	<span class="wrap">
		<button
			class="icon-btn dl"
			class:busy
			class:bad={!!err}
			title={err || t('music.download')}
			aria-label={t('music.download')}
			disabled={busy}
			onclick={go}
		>
			<Icon name="download" {size} />
		</button>
		{#if err}<span class="tip" role="alert">{err}</span>{/if}
	</span>
{/if}

<style>
	.wrap {
		position: relative;
		display: inline-flex;
	}
	.dl {
		width: 32px;
		height: 32px;
		color: var(--text-muted);
	}
	.dl.busy {
		opacity: 0.5;
	}
	.dl.bad {
		color: var(--accent);
	}
	.tip {
		position: absolute;
		right: 0;
		bottom: calc(100% + 4px);
		z-index: 5;
		width: max-content;
		max-width: 280px;
		padding: 6px 10px;
		border-radius: 10px;
		font-size: 12px;
		line-height: 1.5;
		background: var(--surface);
		border: 1px solid var(--line);
		box-shadow: 0 10px 24px -14px var(--shadow);
	}
</style>

