// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
	namespace App {
		interface PageState {
			album?: string; // shallow-routed open album id on /albums
		}
	}
	interface Window {
		__BAILING_API__?: string; // injected by the desktop shell (P2)
		__BAILING_DESKTOP__?: boolean; // set by the desktop shell: no service worker
	}
}

export {};
