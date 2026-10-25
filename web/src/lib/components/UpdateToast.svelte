<script lang="ts">
	// "A new version is available" toast for the installed app (PWA).
	import { t } from '$lib/i18n';
	import { applyUpdate, pwa } from '$lib/pwa.svelte';
</script>

{#if pwa.waiting}
	<div class="toast glass" role="status">
		<span>{t('pwa.update')}</span>
		<button class="btn primary" onclick={applyUpdate}>{t('pwa.reload')}</button>
	</div>
{/if}

<style>
	.toast {
		position: fixed;
		left: 50%;
		top: calc(var(--topbar-h) + 10px);
		transform: translateX(-50%);
		z-index: 70;
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 8px 10px 8px 16px;
		border-radius: 999px;
		box-shadow: 0 16px 40px -20px var(--shadow);
		font-size: 14px;
	}
	.btn {
		font-size: 13px;
		padding: 0.3em 1em;
	}
</style>

