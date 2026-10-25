<script lang="ts">
	// Settings → LX source scripts (admin): upload, enable/disable, delete,
	// reload, and see what each script declared (platforms + qualities).
	import { api, ApiError, type LxState } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { formatBytes } from '$lib/util/format';

	let lxs = $state<LxState | null>(null);
	let busy = $state(false);
	let msg = $state('');
	let fileInput: HTMLInputElement | undefined = $state();

	async function run(p: Promise<LxState>) {
		busy = true;
		msg = '';
		try {
			lxs = await p;
		} catch (e) {
			msg = e instanceof ApiError ? e.message : String(e);
		} finally {
			busy = false;
		}
	}

	$effect(() => {
		run(api<LxState>('/api/lx/scripts'));
	});

	async function onFile(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const f = input.files?.[0];
		input.value = '';
		if (!f) return;
		const content = await f.text();
		run(api<LxState>('/api/lx/scripts', { body: { file: f.name, content } }));
	}

	function toggle(id: string, enabled: boolean) {
		run(api<LxState>(`/api/lx/scripts/${id}`, { method: 'PUT', body: { enabled } }));
	}

	function remove(id: string, name: string) {
		if (!confirm(t('settings.lxConfirmDelete', { n: name }))) return;
		run(api<LxState>(`/api/lx/scripts/${id}`, { method: 'DELETE' }));
	}

	const platName = (code: string) => lxs?.platforms.find((p) => p.code === code)?.name || code;
</script>

{#if lxs && !lxs.enabled}
	<p class="muted small">{t('online.disabled')}</p>
{:else}
	<div class="row">
		<input bind:this={fileInput} type="file" accept=".js,text/javascript" hidden onchange={onFile} />
		<button class="btn" disabled={busy} onclick={() => fileInput?.click()}><Icon name="upload" size={15} />{t('settings.lxUpload')}</button>
		<button class="btn" disabled={busy} onclick={() => run(api<LxState>('/api/lx/reload', { body: {} }))}>
			<Icon name="refresh" size={15} />{t('settings.lxReload')}
		</button>
		{#if busy}<span class="muted small">{t('settings.lxBusy')}</span>{/if}
	</div>
	{#if msg}<p class="warn-text small">{msg}</p>{/if}
	{#if lxs?.dir}<p class="muted small mono">{t('settings.lxDir', { d: lxs.dir })}</p>{/if}

	{#if lxs && !lxs.scripts.length}
		<p class="muted small">{t('settings.lxEmpty')}</p>
	{/if}
	<ul class="scripts">
		{#each lxs?.scripts ?? [] as s (s.id)}
			<li>
				<div class="head">
					<Icon name="code" size={16} />
					<strong>{s.name}</strong>
					{#if s.version}<span class="muted small">v{s.version}</span>{/if}
					{#if s.author}<span class="muted small">· {s.author}</span>{/if}
					<span class="st {s.status}">{t('settings.lxStatus.' + s.status)}</span>
					<span class="grow"></span>
					<label class="sw" title={s.enabled ? t('common.on') : t('common.off')}>
						<input type="checkbox" checked={s.enabled} disabled={busy} onchange={(e) => toggle(s.id, e.currentTarget.checked)} />
					</label>
					<button class="icon-btn" title={t('settings.lxDelete')} disabled={busy} onclick={() => remove(s.id, s.name)}>
						<Icon name="trash" size={15} />
					</button>
				</div>
				{#if s.description}<p class="muted small">{s.description}</p>{/if}
				{#if Object.keys(s.sources).length}
					<p class="plats small">
						<span class="muted">{t('settings.lxPlatforms')}:</span>
						{#each Object.entries(s.sources) as [code, d] (code)}
							<span class="tag">{platName(code)} <em>{(d.qualitys ?? []).join(' / ')}</em></span>
						{/each}
					</p>
				{/if}
				{#if s.error}<p class="warn-text small">{s.error}</p>{/if}
				{#if s.update}<p class="muted small">↻ {s.update}</p>{/if}
				<p class="muted tiny mono">{s.file} · {formatBytes(s.size)}</p>
			</li>
		{/each}
	</ul>
	<p class="muted small">{t('online.risk')}</p>
{/if}

<style>
	.row {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
		margin: 6px 0;
	}
	.small {
		font-size: 12.5px;
	}
	.tiny {
		font-size: 11px;
		margin: 2px 0 0;
	}
	.mono {
		font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
		word-break: break-all;
	}
	.warn-text {
		color: var(--accent);
	}
	.scripts {
		list-style: none;
		margin: 8px 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.scripts li {
		padding: 10px 12px;
		border-radius: var(--radius-m);
		border: 1px solid var(--line);
		background: color-mix(in oklch, var(--surface) 60%, transparent);
	}
	.scripts p {
		margin: 4px 0 0;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 6px;
		flex-wrap: wrap;
	}
	.grow {
		flex: 1;
	}
	.st {
		font-size: 11px;
		padding: 1px 8px;
		border-radius: 999px;
		background: var(--accent-soft);
	}
	.st.ok {
		background: color-mix(in oklch, var(--accent-2) 30%, transparent);
	}
	.st.error {
		background: color-mix(in oklch, var(--accent) 35%, transparent);
	}
	.st.disabled {
		opacity: 0.6;
	}
	.plats {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		align-items: center;
	}
	.tag {
		padding: 1px 8px;
		border-radius: 999px;
		border: 1px solid var(--line);
	}
	.tag em {
		font-style: normal;
		color: var(--text-muted);
		font-size: 11px;
	}
	.sw input {
		width: 34px;
		height: 18px;
		accent-color: var(--accent);
	}
</style>

