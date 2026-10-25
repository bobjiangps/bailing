<script lang="ts">
	// Settings: appearance, photo viewer options, server traffic (billed by
	// volume, so it gets a proper panel), library maintenance and account.
	import { api, type TrafficSummary } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import LxScripts from '$lib/online/LxScripts.svelte';
	import { prefs, PALETTES, SWATCHES, type Mode } from '$lib/stores/prefs.svelte';
	import { pageTraffic } from '$lib/stores/pageTraffic.svelte';
	import { requireLogin, session } from '$lib/stores/session.svelte';
	import { formatBytes, formatTime } from '$lib/util/format';

	type Status = {
		version: string;
		vips: string;
		scanning_photos: boolean;
		scanning_music: boolean;
		queue: number;
		albums: number;
		photos: number;
		tracks: number;
	};

	let traffic = $state<TrafficSummary | null>(null);
	let status = $state<Status | null>(null);
	let rescanMsg = $state('');

	type Dir = { path: string; exists: boolean };
	type Usage = { path: string; bytes: number; files: number };
	type Storage = {
		config_file: string;
		photos: Dir[];
		music: Dir[];
		data: string;
		db_bytes: number;
		photo_cache: Usage;
		cover_cache: Usage;
	};
	let storage = $state<Storage | null>(null);
	let cleanMsg = $state('');
	let cleaning = $state(false);

	async function loadStorage() {
		storage = await api<Storage>('/api/admin/storage').catch(() => null);
	}

	async function cleanCache() {
		cleaning = true;
		try {
			const r = await api<{ files: number; bytes: number }>('/api/admin/cache/clean', { body: {} });
			cleanMsg = t('settings.cacheCleaned', { n: r.files, b: formatBytes(r.bytes) });
			await loadStorage();
		} catch {
			cleanMsg = t('settings.cacheBusy');
		} finally {
			cleaning = false;
			setTimeout(() => (cleanMsg = ''), 6000);
		}
	}

	$effect(requireLogin);

	async function loadTraffic() {
		traffic = await api<TrafficSummary>('/api/traffic/summary').catch(() => null);
	}
	async function loadStatus() {
		status = await api<Status>('/api/admin/status').catch(() => null);
	}

	$effect(() => {
		if (!session.user) return;
		loadTraffic();
		if (session.user.is_admin) {
			loadStatus();
			loadStorage();
		}
		const id = setInterval(loadTraffic, 30000);
		return () => clearInterval(id);
	});

	async function rescan() {
		await api('/api/admin/rescan?what=all', { body: {} }).catch(() => {});
		rescanMsg = t('settings.rescanStarted');
		setTimeout(() => (rescanMsg = ''), 4000);
		setTimeout(loadStatus, 1500);
	}

	const modes: Mode[] = ['light', 'dark', 'system'];
	const maxDay = $derived(Math.max(1, ...(traffic?.days.map((d) => d.bytes) ?? [1])));
	const quotaPct = $derived(
		traffic && traffic.quota_bytes ? Math.min(100, (traffic.month / traffic.quota_bytes) * 100) : 0
	);
	const fmtDate = (s: number) =>
		new Date(s * 1000).toLocaleString(prefs.lang === 'zh' ? 'zh-CN' : 'en', {
			month: 'numeric',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
</script>

<svelte:head><title>{t('settings.title')} · {t('brand')}</title></svelte:head>

<div class="page settings">
	<h1 class="art">{t('settings.title')}</h1>

	<!-- appearance -->
	<section class="card glass">
		<h2>{t('settings.appearance')}</h2>
		<div class="row">
			<span class="k">{t('theme.palette')}</span>
			<div class="pals">
				<button class="pal" class:sel={prefs.palette === 'auto'} onclick={() => prefs.set('palette', 'auto')}>
					<span class="sw auto"></span>{t('palette.auto')}
				</button>
				{#each PALETTES as p (p)}
					<button class="pal" class:sel={prefs.palette === p} onclick={() => prefs.set('palette', p)}>
						<span class="sw">
							{#each SWATCHES[p] as c (c)}<i style:background={c}></i>{/each}
						</span>{t('palette.' + p)}
					</button>
				{/each}
			</div>
		</div>
		<div class="row">
			<span class="k">{t('theme.mode')}</span>
			<div class="seg">
				{#each modes as m (m)}
					<button class:on={prefs.mode === m} onclick={() => prefs.set('mode', m)}>{t('theme.' + m)}</button>
				{/each}
			</div>
		</div>
		<div class="row">
			<span class="k">{t('settings.language')}</span>
			<div class="seg">
				<button class:on={prefs.lang === 'zh'} onclick={() => prefs.set('lang', 'zh')}>中文</button>
				<button class:on={prefs.lang === 'en'} onclick={() => prefs.set('lang', 'en')}>English</button>
			</div>
		</div>
	</section>

	<!-- viewer -->
	<section class="card glass">
		<h2>{t('settings.viewer')}</h2>
		<label class="row switch">
			<span class="k">{t('settings.showExif')}</span>
			<input type="checkbox" checked={prefs.showExif} onchange={(e) => prefs.set('showExif', e.currentTarget.checked)} />
		</label>
		<label class="row switch">
			<span class="k">{t('settings.showGps')}</span>
			<input type="checkbox" checked={prefs.showGps} onchange={(e) => prefs.set('showGps', e.currentTarget.checked)} />
		</label>
	</section>

	<!-- traffic -->
	<section class="card glass" id="traffic">
		<h2><Icon name="server" size={18} /> {t('settings.traffic')}</h2>
		<div class="row">
			<span class="k">{t('traffic.pageTitle')} <strong class="num">{formatBytes(pageTraffic.bytes)}</strong></span>
			<label class="k small">
				{t('settings.pageWarn')}
				<select
					value={prefs.pageWarnMB}
					onchange={(e) => prefs.set('pageWarnMB', +e.currentTarget.value)}
				>
					{#each [100, 200, 500, 1024, 2048] as mb (mb)}
						<option value={mb}>{formatBytes(mb * 1024 * 1024)}</option>
					{/each}
					<option value={0}>{t('common.off')}</option>
				</select>
			</label>
		</div>
		<p class="muted small">{t('traffic.pageFoot')}</p>
		{#if traffic}
			<div class="big">
				<div>
					<span class="muted">{t('settings.today')}</span>
					<strong class="num">{formatBytes(traffic.today)}</strong>
				</div>
				<div>
					<span class="muted">{t('settings.month', { d: traffic.month_start })}</span>
					<strong class="num" class:warn={traffic.warn} class:over={traffic.over}>{formatBytes(traffic.month)}</strong>
				</div>
			</div>

			{#if traffic.quota_bytes}
				<div class="quota">
					<div class="bar" class:warn={traffic.warn}><i style:width="{quotaPct}%"></i></div>
					<span class="muted num">{t('settings.quota', { q: formatBytes(traffic.quota_bytes) })} · {quotaPct.toFixed(1)}%</span>
				</div>
			{:else}
				<p class="muted small">{t('settings.noQuota')}</p>
			{/if}

			<h3>{t('settings.byCategory')}</h3>
			<ul class="cats">
				{#each Object.entries(traffic.by_category).sort((a, b) => b[1] - a[1]) as [cat, b] (cat)}
					<li><span>{t('cat.' + cat)}</span><span class="num">{formatBytes(b)}</span></li>
				{:else}
					<li class="muted">—</li>
				{/each}
			</ul>

			<h3>{t('settings.last30')}</h3>
			<div class="chart" role="img" aria-label={t('settings.last30')}>
				{#each traffic.days as d (d.day)}
					<span class="col" title="{d.day} · {formatBytes(d.bytes)}">
						<i style:height="{Math.max(d.bytes ? 3 : 0, (d.bytes / maxDay) * 100)}%"></i>
					</span>
				{/each}
			</div>

			<h3>{t('settings.recentStreams')}</h3>
			<table class="streams">
				<thead>
					<tr>
						<th></th>
						<th></th>
						<th class="r">{t('settings.duration')}</th>
						<th class="r">{t('settings.size')}</th>
					</tr>
				</thead>
				<tbody>
					{#each traffic.recent_streams as st (st.sid)}
						<tr>
							<td class="muted num">{fmtDate(st.started_at)}</td>
							<td class="ell">
								<span class="tag" class:proxy={st.category === 'proxy'}>{t('cat.' + st.category)}</span>
								{st.title}
							</td>
							<td class="r num">{formatTime(st.last_at - st.started_at)}</td>
							<td class="r num">{formatBytes(st.bytes)}</td>
						</tr>
					{:else}
						<tr><td colspan="4" class="muted">—</td></tr>
					{/each}
				</tbody>
			</table>
			<p class="muted small">{t('settings.overhead')}</p>
		{:else}
			<p class="muted">{t('common.loading')}</p>
		{/if}
	</section>

	<!-- playback & online sources -->
	<section class="card glass" id="playback">
		<h2><Icon name="globe" size={18} /> {t('settings.playback')}</h2>
		<label class="row switch">
			<span class="k">{t('settings.autoRelay')}</span>
			<input type="checkbox" checked={prefs.autoRelay} onchange={(e) => prefs.set('autoRelay', e.currentTarget.checked)} />
		</label>
		<p class="muted small">{t('settings.autoRelayHint')}</p>
		<div class="row">
			<span class="k">{t('settings.lxQuality')}</span>
			<select value={prefs.lxQuality} onchange={(e) => prefs.set('lxQuality', e.currentTarget.value)}>
				{#each ['128k', '320k', 'flac', 'flac24bit'] as q (q)}<option value={q}>{q}</option>{/each}
			</select>
		</div>
	</section>

	<!-- LX scripts (admin) -->
	{#if session.user?.is_admin}
		<section class="card glass" id="lx">
			<h2><Icon name="code" size={18} /> {t('settings.lx')}</h2>
			<LxScripts />
		</section>
	{/if}

	<!-- library (admin) -->
	{#if session.user?.is_admin}
		<section class="card glass">
			<h2>{t('settings.library')}</h2>
			{#if status}
				<p class="muted num small">
					{t('settings.status', {
						v: status.version,
						a: status.albums,
						p: status.photos,
						t: status.tracks,
						q: status.queue
					})}
				</p>
				{#if !status.vips}<p class="warn-text small">{t('settings.vipsMissing')}</p>{/if}
			{/if}
			<div class="row">
				<button class="btn" onclick={rescan}><Icon name="refresh" size={15} />{t('settings.rescan')}</button>
				{#if rescanMsg}<span class="muted small">{rescanMsg}</span>{/if}
			</div>
			{#if storage}
				{#each [{ k: 'settings.photoDirs', list: storage.photos }, { k: 'settings.musicDirs', list: storage.music }] as grp (grp.k)}
					<div class="dirs">
						<span class="k small">{t(grp.k)}</span>
						<ul>
							{#each grp.list as d (d.path)}
								<li class="num small" class:missing={!d.exists}>
									<code>{d.path}</code>
									{#if !d.exists}<span class="warn-text"> · {t('settings.dirMissing')}</span>{/if}
								</li>
							{:else}
								<li class="muted small">—</li>
							{/each}
						</ul>
					</div>
				{/each}
				<p class="muted small">{t('settings.dirsHint', { f: storage.config_file || 'config.yml' })}</p>
			{/if}
		</section>

		{#if storage}
			<section class="card glass">
				<h2>{t('settings.cache')}</h2>
				<table class="usage small num">
					<tbody>
						<tr>
							<th>{t('settings.cachePhoto')}</th>
							<td>{formatBytes(storage.photo_cache.bytes)}</td>
							<td class="muted">{t('settings.cacheFiles', { n: storage.photo_cache.files })}</td>
							<td><code>{storage.photo_cache.path}</code></td>
						</tr>
						<tr>
							<th>{t('settings.cacheCover')}</th>
							<td>{formatBytes(storage.cover_cache.bytes)}</td>
							<td class="muted">{t('settings.cacheFiles', { n: storage.cover_cache.files })}</td>
							<td><code>{storage.cover_cache.path}</code></td>
						</tr>
						<tr>
							<th>{t('settings.db')}</th>
							<td>{formatBytes(storage.db_bytes)}</td>
							<td></td>
							<td><code>{storage.data}</code></td>
						</tr>
					</tbody>
				</table>
				<p class="muted small">{t('settings.cacheHint')}</p>
				<div class="row">
					<button class="btn" disabled={cleaning} onclick={cleanCache}>
						<Icon name="refresh" size={15} />{t('settings.cacheClean')}
					</button>
					{#if cleanMsg}<span class="muted small">{cleanMsg}</span>{/if}
				</div>
			</section>
		{/if}
	{/if}

	<!-- account -->
	{#if session.user && !session.noAuth}
		<section class="card glass">
			<h2>{t('settings.account')}</h2>
			<div class="row">
				<span class="k"><Icon name="user" size={16} /> {session.user.display_name || session.user.name}</span>
				<button class="btn" onclick={() => session.logout()}><Icon name="logout" size={15} />{t('nav.logout')}</button>
			</div>
		</section>
	{/if}
</div>

<style>
	.dirs {
		margin-top: 12px;
	}
	.dirs ul {
		margin: 4px 0 0;
		padding-left: 18px;
	}
	.dirs code,
	.usage code {
		word-break: break-all;
		font-size: 12px;
	}
	.usage {
		width: 100%;
		border-collapse: collapse;
		margin: 6px 0;
	}
	.usage th {
		text-align: left;
		font-weight: 500;
		white-space: nowrap;
	}
	.usage th,
	.usage td {
		padding: 5px 10px 5px 0;
		vertical-align: top;
	}
	@media (max-width: 600px) {
		.usage td:last-child {
			display: none;
		}
	}
	.settings {
		max-width: 760px;
		display: flex;
		flex-direction: column;
		gap: 18px;
	}
	h1 {
		margin: 0 0 6px;
		font-weight: 400;
		font-size: clamp(26px, 4vw, 36px);
	}
	.card {
		border-radius: var(--radius-l);
		padding: 18px 22px;
	}
	h2 {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0 0 12px;
		font-size: 16px;
		font-weight: 600;
	}
	h3 {
		margin: 18px 0 8px;
		font-size: 13px;
		font-weight: 500;
		color: var(--text-muted);
	}
	.row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: 10px;
		padding: 8px 0;
	}
	.k {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.small {
		font-size: 12.5px;
	}
	.pals {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.pal {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 4px 10px 4px 6px;
		border-radius: 999px;
		border: 1px solid var(--line);
		font-size: 13px;
	}
	.pal.sel {
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.sw {
		display: inline-flex;
		border-radius: 999px;
		overflow: hidden;
		width: 30px;
		height: 14px;
	}
	.sw i {
		flex: 1;
	}
	.sw.auto {
		background: linear-gradient(90deg, #efebe6 50%, #161b26 50%);
	}
	.seg {
		display: inline-flex;
		padding: 3px;
		gap: 2px;
		border-radius: 999px;
		background: var(--accent-soft);
	}
	.seg button {
		padding: 3px 14px;
		border-radius: 999px;
		font-size: 13px;
	}
	.seg button.on {
		background: var(--surface);
		box-shadow: 0 2px 8px -4px var(--shadow);
	}
	.switch {
		cursor: pointer;
	}
	.switch input {
		appearance: none;
		width: 40px;
		height: 22px;
		border-radius: 999px;
		background: var(--line);
		position: relative;
		cursor: pointer;
		transition: background var(--dur-fast);
	}
	.switch input::after {
		content: '';
		position: absolute;
		top: 3px;
		left: 3px;
		width: 16px;
		height: 16px;
		border-radius: 50%;
		background: var(--surface);
		box-shadow: 0 1px 3px var(--shadow);
		transition: transform var(--dur-base) var(--ease-soft);
	}
	.switch input:checked {
		background: var(--accent);
	}
	.switch input:checked::after {
		transform: translateX(18px);
	}

	.big {
		display: flex;
		gap: 36px;
		flex-wrap: wrap;
	}
	.big div {
		display: flex;
		flex-direction: column;
		font-size: 12.5px;
	}
	.big strong {
		font-size: 26px;
		font-weight: 500;
	}
	.big strong.warn {
		color: var(--accent);
	}
	.big strong.over {
		color: var(--accent);
		text-decoration: underline wavy;
	}
	.quota {
		margin-top: 10px;
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 12.5px;
	}
	.bar {
		height: 6px;
		border-radius: 6px;
		background: var(--line);
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		background: var(--accent-2);
		border-radius: 6px;
		transition: width var(--dur-slow) var(--ease-soft);
	}
	.bar.warn i {
		background: var(--accent);
	}
	.cats {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 6px 18px;
		font-size: 13.5px;
	}
	.cats li {
		display: flex;
		justify-content: space-between;
		border-bottom: 1px dashed var(--line);
		padding: 2px 0;
	}
	.chart {
		display: flex;
		align-items: flex-end;
		gap: 3px;
		height: 80px;
		padding-bottom: 2px;
		border-bottom: 1px solid var(--line);
	}
	.col {
		flex: 1;
		height: 100%;
		display: flex;
		align-items: flex-end;
	}
	.col i {
		display: block;
		width: 100%;
		border-radius: 3px 3px 0 0;
		background: color-mix(in oklch, var(--accent) 70%, transparent);
		transition: height var(--dur-slow) var(--ease-soft);
	}
	.col:hover i {
		background: var(--accent);
	}
	.streams {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
		table-layout: fixed;
	}
	.streams th {
		font-weight: 400;
		color: var(--text-muted);
		font-size: 12px;
		text-align: left;
	}
	.streams th:first-child {
		width: 110px;
	}
	.streams th.r {
		width: 72px;
	}
	.streams td {
		padding: 5px 0;
		border-bottom: 1px dashed var(--line);
	}
	.r {
		text-align: right !important;
	}
	.ell {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.tag {
		font-size: 11px;
		padding: 0 6px;
		margin-right: 4px;
		border-radius: 999px;
		background: var(--accent-soft);
	}
	.tag.proxy {
		background: var(--accent);
		color: var(--surface);
	}
	.warn-text {
		color: var(--accent);
	}
	select {
		padding: 3px 8px;
		border-radius: 8px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text);
		font: inherit;
	}
	@media (max-width: 520px) {
		.streams th:first-child {
			width: 84px;
		}
	}
</style>

