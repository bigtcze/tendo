/**
 * Counts Unicode code points, matching the server's limits (Go `utf8.RuneCountInString`) and OpenAPI `maxLength`.
 * `String.length` and the HTML `maxlength` attribute count UTF-16 code units, so a character outside the
 * Basic Multilingual Plane (most emoji) would count twice. A lone surrogate counts once, as it is sent as U+FFFD.
 */
export function codePointLength(value: string): number {
  return Array.from(value).length;
}
