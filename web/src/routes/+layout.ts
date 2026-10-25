// Pure single-page app: the Go server returns index.html for every unknown
// path and the router takes over in the browser. No server-side rendering.
export const ssr = false;
export const prerender = false;
export const trailingSlash = 'never';

