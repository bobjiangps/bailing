<script lang="ts">
	// A photo album drawn as a real cloth-bound book (pure CSS, no images
	// except the pasted cover photo).
	import { photoUrl, type Album } from '$lib/api/client';
	import { t } from '$lib/i18n';

	let {
		album,
		index = 0,
		hidden = false,
		onopen
	}: { album: Album; index?: number; hidden?: boolean; onopen: (a: Album, el: HTMLElement) => void } =
		$props();

	let el: HTMLElement | undefined = $state();
	// slight, stable per-album tilt so the shelf feels hand-arranged
	const tilt = $derived(((album.id.charCodeAt(0) + album.id.charCodeAt(5)) % 5) - 2);
</script>

<button
	class="book"
	class:hidden
	bind:this={el}
	style:--cloth={album.color ? `color-mix(in oklch, ${album.color} 55%, var(--book-cloth))` : 'var(--book-cloth)'}
	style:--tilt="{tilt * 0.6}deg"
	style:animation-delay="{Math.min(index, 20) * 40}ms"
	onclick={() => el && onopen(album, el)}
	aria-label={album.title}
>
	<span class="cover">
		<span class="spine"></span>
		<span class="photo">
			{#if album.cover}
				<img src={photoUrl(album.cover, 's')} alt="" loading="lazy" />
			{/if}
			<i class="corner tl"></i><i class="corner tr"></i><i class="corner bl"></i><i class="corner br"></i>
		</span>
		<span class="plate art">{album.title}</span>
	</span>
	<span class="pages"></span>
	<span class="caption">
		<span class="count num">{t('albums.photos', { n: album.count })}</span>
		{#if album.date}<span class="date">· {album.date}</span>{/if}
	</span>
</button>

<style>
	.book {
		position: relative;
		display: block;
		width: 100%;
		aspect-ratio: 3 / 4;
		perspective: 900px;
		animation: enter 700ms var(--ease-soft) both;
		transition: opacity 300ms var(--ease-soft), filter 300ms var(--ease-soft);
	}
	.book.hidden {
		opacity: 0;
	}
	.cover {
		position: absolute;
		inset: 0 4% 8% 0;
		border-radius: 3px 10px 10px 3px;
		background:
			repeating-linear-gradient(45deg, rgb(255 255 255 / 0.05) 0 2px, transparent 2px 4px),
			repeating-linear-gradient(-45deg, rgb(0 0 0 / 0.05) 0 2px, transparent 2px 4px),
			linear-gradient(135deg, color-mix(in oklch, var(--cloth) 92%, #fff), var(--cloth));
		box-shadow:
			inset 0 0 0 1px rgb(255 255 255 / 0.08),
			0 18px 30px -18px var(--shadow),
			0 4px 8px -4px var(--shadow);
		transform: rotate(var(--tilt));
		transform-origin: 50% 100%;
		transition:
			transform 500ms var(--ease-soft),
			box-shadow 500ms var(--ease-soft);
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 14% 12% 0 16%;
		gap: 10%;
		z-index: 1;
	}
	.spine {
		position: absolute;
		left: 0;
		top: 0;
		bottom: 0;
		width: 9%;
		border-radius: 3px 0 0 3px;
		background: linear-gradient(90deg, rgb(0 0 0 / 0.25), rgb(255 255 255 / 0.12) 60%, rgb(0 0 0 / 0.15));
	}
	.pages {
		/* paper edges peeking out on the right */
		position: absolute;
		top: 2.5%;
		bottom: 10%;
		right: 1%;
		width: 6%;
		border-radius: 0 6px 6px 0;
		background: repeating-linear-gradient(90deg, var(--paper) 0 2px, color-mix(in oklch, var(--paper) 80%, #000 6%) 2px 3px);
		box-shadow: 0 6px 12px -8px var(--shadow);
	}
	.photo {
		position: relative;
		flex: none;
		width: 100%;
		aspect-ratio: 1;
		/* aspect-ratio boxes grow to fit their content by default; a tall
		   photo would stretch the frame and push the title off the cover */
		min-height: 0;
		max-height: 62%;
		background: var(--paper);
		box-shadow: 0 6px 14px -8px rgb(0 0 0 / 0.5);
		transform: rotate(-1.5deg);
	}
	.photo img {
		position: absolute;
		inset: 5%;
		width: 90%;
		height: 90%;
		object-fit: cover;
	}
	.corner {
		position: absolute;
		width: 16%;
		height: 16%;
		background: color-mix(in oklch, var(--cloth) 40%, #2a2420);
		opacity: 0.85;
	}
	.tl {
		left: -3%;
		top: -3%;
		clip-path: polygon(0 0, 100% 0, 0 100%);
	}
	.tr {
		right: -3%;
		top: -3%;
		clip-path: polygon(0 0, 100% 0, 100% 100%);
	}
	.bl {
		left: -3%;
		bottom: -3%;
		clip-path: polygon(0 0, 100% 100%, 0 100%);
	}
	.br {
		right: -3%;
		bottom: -3%;
		clip-path: polygon(100% 0, 100% 100%, 0 100%);
	}
	.plate {
		flex: none;
		max-width: 100%;
		padding: 4px 12px;
		border-radius: 3px;
		background: color-mix(in oklch, var(--paper) 88%, transparent);
		color: #3b3a36;
		font-size: clamp(13px, 1.4vw, 16px);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		box-shadow: inset 0 0 0 1px rgb(0 0 0 / 0.08);
	}
	.caption {
		position: absolute;
		left: 0;
		right: 0;
		bottom: 0;
		font-size: 12px;
		color: var(--text-muted);
		text-align: center;
	}
	.book:hover .cover,
	.book:focus-visible .cover {
		transform: translateY(-6px) rotateX(4deg) rotateY(-6deg) rotate(0deg);
		box-shadow:
			inset 0 0 0 1px rgb(255 255 255 / 0.08),
			0 30px 40px -22px var(--shadow),
			0 8px 14px -8px var(--shadow);
	}
	.book:active .cover {
		transform: translateY(-2px) scale(0.99);
	}
	@keyframes enter {
		from {
			opacity: 0;
			transform: translateY(12px);
		}
	}
</style>
