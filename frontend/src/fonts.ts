/**
 * Load the primary Inter face before the app's first layout. Native <select> controls cache text metrics when they
 * are first laid out and do not reliably refresh them when a webfont swaps in; blocking boot on the face we actually
 * render from keeps every control's first layout consistent. The timeout bounds a slow or failed font request so the
 * app still starts (with the system fallback) instead of waiting forever.
 */
export async function loadCriticalFont(): Promise<void> {
  if (!document.fonts?.load) return;
  const timeout = new Promise<void>((resolve) => setTimeout(resolve, 1000));
  await Promise.race([
    document.fonts.load('400 16px "Inter Variable"', 'The quick brown fox'),
    timeout,
  ]).catch(() => undefined);
}
