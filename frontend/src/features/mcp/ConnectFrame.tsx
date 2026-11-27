import type { ReactNode } from 'react';
import { Layout } from '../../app/Layout';
import { useI18n, type MessageKey } from '../../i18n';

/** The auth-shell heading: centered like the sign-in and password-reset screens, and wrapping long client names. */
export const HEADING_CLASS = 'text-center text-h1 leading-8 font-semibold text-balance text-ink [overflow-wrap:anywhere]';

/**
 * A catalog sentence with its values filled in as nodes. Values come from the app or the client (a client name, a
 * login, a host), so each one is wrapped in `bdi`: its own text direction cannot reorder the sentence around it.
 */
export function Fill({ id, values }: { id: MessageKey; values: Record<string, string> }) {
  const { t } = useI18n();
  const parts = t(id).split(/(\{\w+\})/);
  return <>{parts.map((part, index) => {
    const name = /^\{(\w+)\}$/.exec(part)?.[1];
    if (name === undefined) return part;
    return <bdi key={index}>{values[name] ?? ''}</bdi>;
  })}</>;
}

/** The signed-out auth shell rhythm: a centered title, a sentence and a raised panel, like the password-reset screens. */
export function ConnectFrame({ heading, children }: { heading: ReactNode; children: ReactNode }) {
  return <Layout>
    <section className="settle mx-auto w-full max-w-md lg:mt-10">
      {heading}
      {children}
    </section>
  </Layout>;
}

/** Plain link back to Home; `onHome` routes inside the app, the href keeps it a real link for assistive tech. */
export function ConnectHomeLink({ onHome }: { onHome: () => void }) {
  const { t } = useI18n();
  return <a
    href="/"
    onClick={(event) => { event.preventDefault(); onHome(); }}
    className="inline-flex min-h-11 items-center rounded-xl px-3 text-small font-semibold text-muted hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
  >
    {t('nav.home')}
  </a>;
}
