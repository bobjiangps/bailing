<script lang="ts">
	// Slim banner shown when the monthly server traffic passes the warning
	// threshold configured in config.yaml (traffic.monthly_quota_gb / warn_percent).
	import { base } from '$app/paths';
	import { api } from '$lib/api/client';
	import { t } from '$lib/i18n';
	import { session } from '$lib/stores/session.svelte';
	import { formatBytes } from '$lib/util/format';

	type Info = { warn: boolean; over: boolean; month: number; quota_bytes: number };
	let info = $state<Info | null>(null);
	let dismissed = $state(false);

	$effect(() => {
		if (!session.user) {
			info = null;
			return;
		}
		const load = () =>
			api<{ traffic?: Info }>('/api/home')
				.then((r) => (info = r.traffic ?? null))
				.catch(() => {});
		load();
		const timer = setInterval(load, 10 * 60 * 1000);
		return () => clearInterval(timer);
	});

	const pct = $derived(info && info.quota_bytes ? Math.round((info.month / info.quota_bytes) * 100) : 0);
</script>

{#if info?.warn && !dismissed}
	<div class="banner" class:over={info.over} role="status">
		<a href="{base}/settings#traffic">
			{info.over
				? t('banner.over', { used: formatBytes(info.month), quota: formatBytes(info.quota_bytes) })
				: t('banner.warn', { p: pct, used: formatBytes(info.month), quota: formatBytes(info.quota_bytes) })}
		</a>
		<button aria-label="dismiss" onclick={() => (dismissed = true)}>×</button>
	</div>
{/if}

<style>
	.banner {
		position: fixed;
		top: calc(var(--topbar-h) - 4px);
		left: 50%;
		transform: translateX(-50%);
		z-index: 31;
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 4px 8px 4px 14px;
		border-radius: 999px;
		font-size: 12.5px;
		background: color-mix(in oklch, var(--accent) 20%, var(--surface));
		border: 1px solid color-mix(in oklch, var(--accent) 40%, transparent);
		box-shadow: 0 10px 30px -18px var(--shadow);
		animation: drop 500ms var(--ease-soft);
	}
	.banner.over {
		background: var(--accent);
		color: var(--surface);
	}
	button {
		width: 22px;
		height: 22px;
		border-radius: 50%;
		font-size: 16px;
		line-height: 1;
	}
	@keyframes drop {
		from {
			opacity: 0;
			transform: translate(-50%, -10px);
		}
	}
</style>

