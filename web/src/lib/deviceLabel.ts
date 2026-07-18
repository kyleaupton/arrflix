// A friendly label from a stored User-Agent, e.g. "Chrome on macOS". Best-effort:
// an unknown or missing UA still yields a sensible label rather than a raw string.
// The UA is captured server-side at login/subscribe, so it can't be spoofed to
// impersonate another device's name.
export function deviceLabel(ua: string | null): string {
  if (!ua) return 'Unknown device'
  const browser = /edg/i.test(ua)
    ? 'Edge'
    : /chrome|crios/i.test(ua)
      ? 'Chrome'
      : /firefox|fxios/i.test(ua)
        ? 'Firefox'
        : /safari/i.test(ua)
          ? 'Safari'
          : 'Browser'
  const os = /android/i.test(ua)
    ? 'Android'
    : /iphone|ipad|ipod/i.test(ua)
      ? 'iOS'
      : /mac os/i.test(ua)
        ? 'macOS'
        : /windows/i.test(ua)
          ? 'Windows'
          : /linux/i.test(ua)
            ? 'Linux'
            : ''
  return os ? `${browser} on ${os}` : browser
}
