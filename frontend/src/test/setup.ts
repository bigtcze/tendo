import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, beforeEach } from 'vitest';

function describeDom(): string {
  const active = document.activeElement;
  const focus = active ? `${active.tagName.toLowerCase()}[${(active.getAttribute('aria-label') ?? active.textContent ?? '').trim().slice(0, 80)}]` : 'none';
  const landmarks = [...document.querySelectorAll('h1, h2, [role="status"], [role="alert"], form[aria-label]')]
    .map((node) => (node.getAttribute('aria-label') ?? node.textContent ?? '').trim().slice(0, 80)).filter(Boolean).slice(0, 12);
  return `path=${window.location.pathname}${window.location.search} activeElement=${focus} bodyChildren=${document.body.childElementCount} visible=${JSON.stringify(landmarks)}`;
}

// Failure-time evidence for intermittent CI timeouts (STATE triage). Hooks such as cleanup run before
// onTestFailed, so the DOM phase is snapshotted shortly before the test's own timeout and printed only on failure.
beforeEach(({ task, onTestFailed }) => {
  const timeout = task.timeout ?? 0;
  let snapshot = '';
  const timer = timeout > 500 ? setTimeout(() => { snapshot = describeDom(); }, timeout - 250) : undefined;
  onTestFailed(() => {
    console.error(`[failure-diagnostics] ${task.name}: at failure ${describeDom()}${snapshot ? ` | before timeout ${snapshot}` : ''}`);
  });
  return () => clearTimeout(timer);
});

afterEach(() => {
  cleanup();
  localStorage.clear();
  document.documentElement.lang = 'en';
  window.history.replaceState(null, '', '/');
});

// Node's Request rejects relative URLs (browsers resolve them against the page origin).
// Mirror browser behavior so the real openapi-fetch client works with baseUrl ''.
const NativeRequest = globalThis.Request;
class BrowserLikeRequest extends NativeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === 'string' ? new URL(input, window.location.href).href : input, init);
  }
}
globalThis.Request = BrowserLikeRequest;
