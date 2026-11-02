export function installDialogPolyfill() {
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
    configurable: true,
    value(this: HTMLDialogElement) {
      this.open = true;
      this.setAttribute('open', '');
      const focusable = this.querySelector<HTMLElement>(
        [
          '[data-dialog-content] button:not([disabled])',
          '[data-dialog-content] input:not([disabled])',
          '[data-dialog-content] select:not([disabled])',
          '[data-dialog-content] textarea:not([disabled])',
          '[data-dialog-content] [tabindex]:not([tabindex="-1"])',
        ].join(', '),
      );
      (focusable ?? this.querySelector<HTMLElement>('h2'))?.focus();
    },
  });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', {
    configurable: true,
    value(this: HTMLDialogElement) {
      this.open = false;
      this.removeAttribute('open');
      queueMicrotask(() => this.dispatchEvent(new Event('close')));
    },
  });
}
