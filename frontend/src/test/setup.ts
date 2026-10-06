import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

afterEach(() => {
  cleanup();
  localStorage.clear();
  document.documentElement.lang = 'en';
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
