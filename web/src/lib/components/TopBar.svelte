<script lang="ts">
	import { features } from '$lib/features.svelte';
	import { base } from '$app/paths';
	import { page } from '$app/state';
	import Icon from './Icon.svelte';
	import { t } from '$lib/i18n';
	import { prefs, PALETTES, SWATCHES, type Mode } from '$lib/stores/prefs.svelte';
	import { session } from '$lib/stores/session.svelte';
	import { player } from '$lib/player/player.svelte';
	import TrafficChip from '$lib/player/TrafficChip.svelte';

	let paletteOpen = $state(false);
	let userOpen = $state(false);
	let innerWidth = $state(1024);
	const narrow = $derived(innerWidth < 640);

	const modes: Mode[] = ['light', 'dark', 'system'];
	const modeIcon = { light: 'sun', dark: 'moon', system: 'system' } as const;

	function cycleMode() {
		prefs.set('mode', modes[(modes.indexOf(prefs.mode) + 1) % modes.length]);
	}

	const path = $derived(page.url.pathname.slice(base.length) || '/');

	function closeMenus(e: MouseEvent) {
		if (!(e.target as HTMLElement).closest('.menu-wrap')) {
			paletteOpen = false;
			userOpen = false;
		}
	}
</script>

<svelte:window onclick={closeMenus} bind:innerWidth />

<header class="topbar">
	<a class="brand art" href="{base}/">
		<span class="dot"></span>{t('brand')}
	</a>

	{#if session.user}
		<nav>
			{#if features.photos}<a href="{base}/albums" class:on={path.startsWith('/albums')}>{t('nav.albums')}</a>{/if}
			{#if features.music}<a href="{base}/music" class:on={path.startsWith('/music')}>{t('nav.music')}</a>{/if}
		</nav>
	{/if}

	<div class="tools">
		{#if session.user && !player.current}
			<!-- the player bar carries this chip while something is queued -->
			<span class="tchip"><TrafficChip drop="down" compact={narrow} /></span>
		{/if}
		<div class="menu-wrap">
			<button
				class="icon-btn"
				title={t('theme.palette')}
				aria-label={t('theme.palette')}
				onclick={() => ((paletteOpen = !paletteOpen), (userOpen = false))}
			>
				<Icon name="palette" />
			</button>
			{#if paletteOpen}
				<div class="menu glass" role="menu">
					<button class="pal" class:sel={prefs.palette === 'auto'} onclick={() => prefs.set('palette', 'auto')}>
						<span class="sw auto"></span>{t('palette.auto')}
					</button>
					{#each PALETTES as p (p)}
						<button class="pal" class:sel={prefs.palette === p} onclick={() => prefs.set('palette', p)}>
							<span class="sw">
								{#each SWATCHES[p] as c (c)}<i style:background={c}></i>{/each}
							</span>
							{t('palette.' + p)}
						</button>
					{/each}
				</div>
			{/if}
		</div>

		<button
			class="icon-btn"
			title="{t('theme.mode')}: {t('theme.' + prefs.mode)}"
			aria-label={t('theme.mode')}
			onclick={cycleMode}
		>
			<Icon name={modeIcon[prefs.mode]} />
		</button>

		<button class="icon-btn lang" title={t('lang.switch')} onclick={() => prefs.set('lang', prefs.lang === 'zh' ? 'en' : 'zh')}>
			{prefs.lang === 'zh' ? 'EN' : '中'}
		</button>

		{#if session.user}
			<div class="menu-wrap">
				<button
					class="icon-btn avatar"
					aria-label={session.user.display_name}
					onclick={() => ((userOpen = !userOpen), (paletteOpen = false))}
				>
					{session.user.display_name.slice(0, 1).toUpperCase()}
				</button>
				{#if userOpen}
					<div class="menu glass right" role="menu">
						<div class="who">{session.user.display_name}</div>
						<a class="item" href="{base}/settings" onclick={() => (userOpen = false)}>
							<Icon name="settings" size={16} />{t('nav.settings')}
						</a>
						{#if !session.noAuth}
							<button class="item" onclick={() => session.logout()}>
								<Icon name="logout" size={16} />{t('nav.logout')}
							</button>
						{/if}
					</div>
				{/if}
			</div>
		{:else if session.ready && path !== '/login'}
			<a class="btn login" href="{base}/login">{t('nav.login')}</a>
		{/if}
	</div>
</header>

<style>
	.topbar {
		position: fixed;
		top: 0;
		left: 0;
		right: 0;
		z-index: 30;
		height: var(--topbar-h);
		display: flex;
		align-items: center;
		gap: 24px;
		padding: 0 max(20px, env(safe-area-inset-right)) 0 max(20px, env(safe-area-inset-left));
		background: linear-gradient(to bottom, color-mix(in oklch, var(--bg) 85%, transparent), transparent);
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 20px;
	}
	.dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		background: linear-gradient(135deg, var(--accent), var(--accent-2));
		box-shadow: 0 0 0 4px var(--accent-soft);
	}
	nav {
		display: flex;
		gap: 18px;
	}
	nav a {
		position: relative;
		color: var(--text-muted);
		transition: color var(--dur-fast);
	}
	nav a.on,
	nav a:hover {
		color: var(--text);
	}
	nav a.on::after {
		content: '';
		position: absolute;
		left: 20%;
		right: 20%;
		bottom: -4px;
		height: 2px;
		border-radius: 2px;
		background: var(--accent);
	}
	.tools {
		margin-left: auto;
		display: flex;
		align-items: center;
		gap: 4px;
	}
	.tchip {
		margin-right: 6px;
	}
	.lang {
		font-size: 13px;
		font-weight: 600;
	}
	.avatar {
		background: var(--accent-soft);
		font-weight: 600;
	}
	.login {
		padding: 0.35em 1em;
		margin-left: 6px;
	}
	.menu-wrap {
		position: relative;
	}
	.menu {
		position: absolute;
		top: 46px;
		right: -60px;
		min-width: 220px;
		padding: 8px;
		border-radius: var(--radius-m);
		box-shadow: 0 18px 40px -20px var(--shadow);
		display: flex;
		flex-direction: column;
		gap: 2px;
		animation: pop var(--dur-base) var(--ease-soft);
	}
	.menu.right {
		right: 0;
		min-width: 180px;
	}
	.pal,
	.item {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 10px;
		border-radius: 10px;
		text-align: left;
		font-size: 14px;
	}
	.pal:hover,
	.item:hover,
	.pal.sel {
		background: var(--accent-soft);
	}
	.sw {
		display: inline-flex;
		width: 42px;
		height: 18px;
		border-radius: 9px;
		overflow: hidden;
		border: 1px solid var(--line);
	}
	.sw i {
		flex: 1;
	}
	.sw.auto {
		background: linear-gradient(90deg, #efebe6 50%, #161b26 50%);
	}
	.who {
		padding: 6px 10px;
		font-size: 13px;
		color: var(--text-muted);
	}
	@keyframes pop {
		from {
			opacity: 0;
			transform: translateY(-6px) scale(0.98);
		}
	}
	@media (max-width: 640px) {
		.topbar {
			gap: 14px;
		}
		nav {
			gap: 12px;
		}
		.brand {
			font-size: 18px;
		}
	}
</style>
