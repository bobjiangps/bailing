<script lang="ts">
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError } from '$lib/api/client';
	import { t } from '$lib/i18n';
	import { session } from '$lib/stores/session.svelte';

	let name = $state('');
	let password = $state('');
	let remember = $state(true);
	let busy = $state(false);
	let err = $state('');

	// only allow same-app relative targets (no open redirect)
	const next = $derived.by(() => {
		const n = page.url.searchParams.get('next') ?? '';
		return n.startsWith(base + '/') && !n.startsWith('//') ? n : base + '/';
	});

	$effect(() => {
		if (session.user) goto(next, { replaceState: true });
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !name || !password) return;
		busy = true;
		err = '';
		try {
			await session.login(name.trim(), password, remember);
			goto(next, { replaceState: true });
		} catch (e) {
			const code = e instanceof ApiError ? e.code : '';
			err = code === 'locked' ? 'login.locked' : code === 'bad_credentials' ? 'login.bad' : 'login.error';
			password = '';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>{t('nav.login')} · {t('brand')}</title></svelte:head>

<div class="wrap">
	<form class="card glass" onsubmit={submit} class:shake={!!err}>
		<h1 class="art">{t('login.title')}</h1>
		<p class="muted">{t('login.subtitle')}</p>

		<label>
			<span>{t('login.name')}</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input bind:value={name} autocomplete="username" autocapitalize="off" spellcheck="false" autofocus required />
		</label>
		<label>
			<span>{t('login.password')}</span>
			<input type="password" bind:value={password} autocomplete="current-password" required />
		</label>
		<label class="check">
			<input type="checkbox" bind:checked={remember} />
			<span>{t('login.remember')}</span>
		</label>

		{#if err}<p class="err" role="alert">{t(err)}</p>{/if}

		<button class="btn primary" type="submit" disabled={busy}>
			{busy ? t('common.loading') : t('login.submit')}
		</button>
	</form>
</div>

<style>
	.wrap {
		display: grid;
		place-items: center;
		min-height: 100dvh;
		padding: var(--topbar-h) 16px 24px;
	}
	.card {
		width: min(380px, 100%);
		padding: 34px 30px 30px;
		border-radius: var(--radius-l);
		display: flex;
		flex-direction: column;
		gap: 14px;
		box-shadow: 0 30px 80px -40px var(--shadow);
		animation: in 700ms var(--ease-soft) both;
	}
	h1 {
		margin: 0;
		font-weight: 400;
		font-size: 30px;
	}
	.card > p {
		margin: -8px 0 6px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 13px;
		color: var(--text-muted);
	}
	input:not([type='checkbox']) {
		padding: 10px 14px;
		border-radius: var(--radius-s);
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text);
		font-size: 15px;
		transition: border-color var(--dur-fast);
	}
	input:focus {
		outline: none;
		border-color: var(--accent);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 8px;
	}
	.check input {
		accent-color: var(--accent);
	}
	.err {
		margin: 0;
		color: var(--accent);
		font-size: 13.5px;
	}
	.btn {
		margin-top: 6px;
		padding: 0.7em 1.1em;
	}
	.btn:disabled {
		opacity: 0.6;
	}
	.shake {
		animation: shake 380ms var(--ease-soft);
	}
	@keyframes in {
		from {
			opacity: 0;
			transform: translateY(18px) scale(0.98);
		}
	}
	@keyframes shake {
		25% {
			transform: translateX(-6px);
		}
		75% {
			transform: translateX(6px);
		}
	}
</style>

