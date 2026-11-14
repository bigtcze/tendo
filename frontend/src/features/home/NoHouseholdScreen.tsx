import { useRef } from 'react';
import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { Notice } from '../../components/ui/notice';
import { useI18n, type MessageKey } from '../../i18n';
import { useDiscardAccount, type DiscardFailure } from '../auth/useDiscardAccount';

const failureKeys: Record<Exclude<DiscardFailure, null>, MessageKey> = {
  notDiscardable: 'home.noHousehold.discard.notPossible',
  disabled: 'home.noHousehold.discard.disabled',
  rateLimited: 'home.noHousehold.discard.rateLimited',
  unavailable: 'home.noHousehold.discard.failed',
};

/**
 * Signed in, but not in a household yet: the account needs an invitation from a household
 * administrator. Nothing here links to household screens that would only answer 404.
 */
export function NoHouseholdScreen({ onSignedOut, onDiscarded }: { onSignedOut: () => void; onDiscarded: () => void }) {
  const { t } = useI18n();
  const discard = useDiscardAccount({ onSignedOut, onDiscarded });
  const triggerRef = useRef<HTMLButtonElement>(null);

  return (
    <section className="settle mx-auto w-full max-w-3xl">
      <Heading className="text-h1 leading-tight text-ink">{t('home.noHousehold.title')}</Heading>
      <p className="mt-2 max-w-prose text-body text-muted">{t('home.noHousehold.body')}</p>
      {/* Flat sections separated by a rule, as on Account: the screen is two short explanations, not two boxes. */}
      <div className="mt-6 border-t border-line pt-6">
        <h2 className="text-h2 text-ink">{t('home.noHousehold.how.title')}</h2>
        <p className="mt-1 max-w-prose text-body text-muted">{t('home.noHousehold.how.body')}</p>
      </div>
      <section aria-labelledby="no-household-account-heading" className="mt-6 border-t border-line pt-6">
        <h2 id="no-household-account-heading" className="text-h2 text-ink">{t('home.noHousehold.discard.title')}</h2>
        <p className="mt-1 max-w-prose text-body text-muted">{t('home.noHousehold.discard.body')}</p>
        {discard.failure ? <Notice tone="error" className="mt-4">{t(failureKeys[discard.failure])}</Notice> : null}
        <Button ref={triggerRef} variant="secondary" className="mt-4 w-full whitespace-normal text-center sm:w-auto" onClick={discard.openDialog}>
          {t('home.noHousehold.discard.action')}
        </Button>
      </section>
      <Dialog
        open={discard.open}
        onOpenChange={(open) => { if (!open) discard.closeDialog(); }}
        title={t('home.noHousehold.discard.confirmTitle')}
        busy={discard.pending}
        triggerRef={triggerRef}
      >
        <p className="text-body text-muted">{t('home.noHousehold.discard.confirmBody')}</p>
        <div className="mt-5 flex flex-wrap gap-2">
          <Button disabled={discard.pending} onClick={discard.confirm}>
            {discard.pending ? t('home.noHousehold.discard.pending') : t('home.noHousehold.discard.confirm')}
          </Button>
          <Button variant="quiet" disabled={discard.pending} onClick={discard.closeDialog}>
            {t('home.noHousehold.discard.cancel')}
          </Button>
        </div>
      </Dialog>
    </section>
  );
}
