<script lang="ts">
	// Sound panel: playback speed, sleep timer, equaliser and spectrum.
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { prefs } from '$lib/stores/prefs.svelte';
	import { formatTime } from '$lib/util/format';
	import { EQ_BANDS, EQ_PRESETS, fx } from './fx.svelte';
	import { player, RATES, SLEEP_MINUTES } from './player.svelte';

	let { onclose }: { onclose: () => void } = $props();

	const bandLabel = (f: number) => (f >= 1000 ? `${f / 1000}k` : String(f));

	function setEqOn(on: boolean) {
		if (on && !player.enableFx()) return;
		prefs.set('eqOn', on);
		fx.setEq(on, prefs.eq);
	}

	function preset(name: string) {
		const g = EQ_PRESETS[name];
		if (!g) return;
		prefs.set('eqPreset', name);
		prefs.set('eq', [...g]);
		if (!prefs.eqOn) setEqOn(true);
		else fx.setEq(true, prefs.eq);
	}

	function band(i: number, v: number) {
		const g = [...prefs.eq];
		g[i] = v;
		prefs.set('eq', g);
		prefs.set('eqPreset', 'custom');
		fx.setEq(prefs.eqOn, g);
	}

	function setSpectrum(on: boolean) {
		if (on && !player.enableFx()) return;
		prefs.set('spectrum', on);
	}
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<div class="panel glass" role="dialog" aria-label={t('fx.title')}>
	<header>
		<span class="art">{t('fx.title')}</span>
		<button class="icon-btn" aria-label={t('albums.close')} onclick={onclose}><Icon name="x" size={16} /></button>
	</header>

	<section>
		<h4>{t('fx.speed')}</h4>
		<div class="chips">
			{#each RATES as r (r)}
				<button class:on={prefs.rate === r} disabled={player.live} onclick={() => player.setRate(r)}>{r}×</button>
			{/each}
		</div>
		{#if player.live}<p class="note muted">{t('fx.speedLive')}</p>{/if}
	</section>

	<section>
		<h4>
			{t('fx.sleep')}
			{#if player.sleepAt}<span class="left num">· {formatTime(player.sleepLeft)}</span>{/if}
			{#if player.sleepAfterTrack}<span class="left">· {t('fx.sleepTrackOn')}</span>{/if}
		</h4>
		<div class="chips">
			<button class:on={!player.sleepAt && !player.sleepAfterTrack} onclick={() => player.setSleep(0)}>{t('fx.off')}</button>
			{#each SLEEP_MINUTES as m (m)}
				<button onclick={() => player.setSleep(m)}>{t('fx.minutes', { n: m })}</button>
			{/each}
			<button class:on={player.sleepAfterTrack} disabled={player.live} onclick={() => player.setSleep('track')}>{t('fx.sleepTrack')}</button>
		</div>
	</section>

	<section>
		<h4>
			<label class="sw"><input type="checkbox" checked={prefs.eqOn} onchange={(e) => setEqOn(e.currentTarget.checked)} />{t('fx.eq')}</label>
		</h4>
		<div class="chips">
			{#each Object.keys(EQ_PRESETS) as p (p)}
				<button class:on={prefs.eqOn && prefs.eqPreset === p} onclick={() => preset(p)}>{t('fx.preset.' + p)}</button>
			{/each}
		</div>
		<div class="bands" class:dim={!prefs.eqOn}>
			{#each EQ_BANDS as f, i (f)}
				<label>
					<input
						type="range"
						min="-12"
						max="12"
						step="1"
						value={prefs.eq[i]}
						disabled={!prefs.eqOn}
						aria-label="{bandLabel(f)} Hz"
						oninput={(e) => band(i, +e.currentTarget.value)}
					/>
					<span class="num">{bandLabel(f)}</span>
				</label>
			{/each}
		</div>
	</section>

	<section>
		<h4><label class="sw"><input type="checkbox" checked={prefs.spectrum} onchange={(e) => setSpectrum(e.currentTarget.checked)} />{t('fx.spectrum')}</label></h4>
	</section>

	{#if fx.unavailable}
		<p class="note warn">{t('fx.unavailable')}</p>
	{:else}
		<p class="note muted">{t('fx.corsNote')}</p>
	{/if}
</div>

<style>
	.panel {
		position: fixed;
		right: 14px;
		bottom: calc(var(--playerbar-h) + 6px + env(safe-area-inset-bottom));
		z-index: 40;
		width: min(380px, calc(100vw - 28px));
		max-height: min(70dvh, 620px);
		overflow: auto;
		padding: 12px 14px;
		border-radius: var(--radius-l);
		box-shadow: 0 24px 60px -30px var(--shadow);
		animation: rise 260ms var(--ease-soft);
	}
	header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		font-size: 16px;
	}
	section {
		padding: 8px 0;
		border-top: 1px solid var(--line);
	}
	section:first-of-type {
		border-top: 0;
	}
	h4 {
		margin: 0 0 8px;
		font-size: 13px;
		font-weight: 500;
		color: var(--text-muted);
	}
	.left {
		color: var(--accent);
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
	}
	.chips button {
		padding: 2px 10px;
		border-radius: 999px;
		border: 1px solid var(--line);
		font-size: 12.5px;
	}
	.chips button.on {
		background: var(--accent-soft);
		border-color: transparent;
		color: var(--text);
	}
	.chips button:disabled {
		opacity: 0.4;
		cursor: default;
	}
	.sw {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		cursor: pointer;
		color: var(--text);
	}
	.bands {
		display: grid;
		grid-template-columns: repeat(10, 1fr);
		gap: 2px;
		margin-top: 10px;
		height: 130px;
	}
	.bands.dim {
		opacity: 0.45;
	}
	.bands label {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		font-size: 10px;
		color: var(--text-muted);
	}
	.bands input {
		writing-mode: vertical-lr;
		direction: rtl;
		width: 18px;
		flex: 1;
		margin: 0;
	}
	.note {
		margin: 6px 0 0;
		font-size: 11.5px;
		line-height: 1.6;
	}
	.warn {
		color: var(--accent);
	}
	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(10px);
		}
	}
</style>

