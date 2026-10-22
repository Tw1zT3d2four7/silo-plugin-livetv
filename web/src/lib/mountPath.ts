// Silo exposes plugin pages and HTTP routes beneath an installation-scoped
// content prefix. Current hosts use /api/v2/plugin-content/plugins/{id}; keep
// the older /api/v1/plugins/{id} prefix supported for compatible deployments.
// The installation ID is discovered at runtime, not baked into the build.
// Return an empty prefix on the local dev server.
export function extractMountPath(pathname: string): string {
  const m = pathname.match(/^(\/api\/(?:v2\/plugin-content|v1)\/plugins\/\d+)/);
  return m ? m[1] : '';
}

export function mountPath(): string {
  return extractMountPath(window.location.pathname);
}
