/**
 * Load the primary (latin) Inter face before the app's first layout. Native <select> controls cache text metrics when
 * they are first laid out and, in Chromium, do not refresh them when a webfont swaps in; waiting for the face we render
 * from keeps that first layout consistent when it loads within the budget. The timeout bounds a slow or failed font
 * request so the app still starts on the system fallback instead of waiting forever. latin-ext is not awaited.
 */
export async function loadCriticalFont(timeoutMs = 1000): Promise<void> {
  if (!document.fonts?.load) return;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<void>((resolve) => { timer = setTimeout(resolve, timeoutMs); });
  try {
    await Promise.race([document.fonts.load('400 16px "Inter Variable"', 'The quick brown fox'), timeout]);
  } catch {
    // A failed font load must never block startup; the CSS fallback stack renders instead.
  } finally {
    clearTimeout(timer);
  }
}
