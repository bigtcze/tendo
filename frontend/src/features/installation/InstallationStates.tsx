import type { RefObject } from 'react';
import { buttonLook } from '../../components/ui/button';
import type { Translate } from '../../i18n';

/** The one quiet secondary action on an installation screen: back to the household list. */
export const BACK_LINK = buttonLook({ variant: 'secondary', className: 'mt-4' });

/**
 * The installation grant is gone (or was never there). The server refused before any lookup, so nothing about a
 * household is shown: a plain section like the household Admin center's member view, not a card. The heading takes
 * `tabIndex={-1}` so a screen whose grant is refused mid-action can place the reader on it instead of the page body.
 */
export function InstallationOnlyAdmins({ t, headingRef }: { t: Translate; headingRef?: RefObject<HTMLHeadingElement | null> }) {
  return <section aria-labelledby="install-restricted-heading" className="mt-6 sm:mt-8">
    <h2 id="install-restricted-heading" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">
      {t('install.onlyAdmins.title')}
    </h2>
    <p className="mt-1 max-w-prose text-body text-muted">{t('install.onlyAdmins.body')}</p>
  </section>;
}

/**
 * The household the address names is gone. There is nothing to retry and nothing to show; the page keeps the same
 * quiet way back it offers in every state.
 */
export function InstallationGone({ t }: { t: Translate }) {
  return <section aria-labelledby="install-gone-heading" className="mt-6 sm:mt-8">
    <h2 id="install-gone-heading" className="text-h2 text-ink">{t('install.gone.title')}</h2>
    <p className="mt-1 max-w-prose text-body text-muted">{t('install.gone.body')}</p>
  </section>;
}
