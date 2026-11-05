export function installDialogPolyfill() {
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
    configurable: true,
    value(this: HTMLDialogElement) {
      this.open = true;
      this.setAttribute('open', '');
      const focusable = this.querySelector<HTMLElement>('[autofocus]') ?? this.querySelector<HTMLElement>(
        [
          'a[href]',
          'button:not([disabled])',
          'input:not([disabled])',
          'select:not([disabled])',
          'textarea:not([disabled])',
          '[tabindex]:not([tabindex="-1"])',
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
