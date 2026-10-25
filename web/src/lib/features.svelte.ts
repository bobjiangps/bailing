import type { Features } from '$lib/api/client';

/**
 * Which halves of the app are available.
 *
 * Two switches combine:
 *  - build time: VITE_BAILING_APP=all | photos | music (PWA / desktop
 *    packages that only contain one half; see docs/dev-guide.md);
 *  - run time: the server's `features` section in config.yaml.
 * A half is shown only when both allow it.
 */
export type AppKind = 'all' | 'photos' | 'music';

const raw = (import.meta.env.VITE_BAILING_APP ?? 'all') as string;
export const buildApp: AppKind = raw === 'photos' || raw === 'music' ? raw : 'all';

class FeatureFlags {
	server = $state<Features>({ photos: true, music: true });

	photos = $derived(buildApp !== 'music' && this.server.photos);
	music = $derived(buildApp !== 'photos' && this.server.music);
	/** only one half is available: home page leads straight into it */
	single = $derived(this.photos !== this.music);

	set(f: Partial<Features> | undefined) {
		if (f) this.server = { photos: f.photos !== false, music: f.music !== false };
	}
}

export const features = new FeatureFlags();

