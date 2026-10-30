import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../../i18n';
import { SubjectForm, type SubjectValues } from './SubjectForm';

const initial: SubjectValues = { name: 'Anna', type: 'person', linkedUserId: null };

function renderSubjectForm(props: Partial<Parameters<typeof SubjectForm>[0]> = {}) {
  return render(
    <I18nProvider>
      <SubjectForm
        label="Edit Anna"
        initial={initial}
        submitLabel="Save"
        pendingLabel="Saving…"
        onSubmit={async () => ({ kind: 'done' })}
        onCancel={() => undefined}
        {...props}
      />
    </I18nProvider>,
  );
}

describe('SubjectForm behavior', () => {
  it('shows the pending-attempt label while a locked submission is unresolved', async () => {
    let resolveSubmit: (() => void) | undefined;
    const pendingSubmit = new Promise<{ kind: 'done' }>((resolve) => {
      resolveSubmit = () => resolve({ kind: 'done' });
    });
    renderSubjectForm({
      locked: true,
      retryLabel: 'Check again',
      onSubmit: () => pendingSubmit,
    });

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }));

    expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled();
    await act(async () => { resolveSubmit?.(); await pendingSubmit; });
  });

  it('ignores a synchronous duplicate submit, restores controls, and reports rejected submissions', async () => {
    let resolveSubmit: (() => void) | undefined;
    const pendingSubmit = new Promise<{ kind: 'done' }>((resolve) => {
      resolveSubmit = () => resolve({ kind: 'done' });
    });
    const onSubmit = vi.fn(() => pendingSubmit);
    const onCancel = vi.fn();
    const { rerender } = renderSubjectForm({ onSubmit, onCancel });
    const form = screen.getByRole('form', { name: 'Edit Anna' });

    act(() => {
      fireEvent.submit(form);
      fireEvent.submit(form);
    });

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(within(form).getByRole('button', { name: 'Saving…' })).toBeDisabled();
    await act(async () => { resolveSubmit?.(); await pendingSubmit; });
    expect(within(form).getByRole('button', { name: 'Save' })).toBeEnabled();
    expect(within(form).getByRole('button', { name: 'Cancel' })).toBeEnabled();

    rerender(
      <I18nProvider>
        <SubjectForm label="Edit Anna" initial={initial} submitLabel="Save" pendingLabel="Saving…"
          onSubmit={() => Promise.reject(new Error('offline'))} onCancel={onCancel} />
      </I18nProvider>,
    );
    fireEvent.submit(screen.getByRole('form', { name: 'Edit Anna' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Couldn’t save that. Check your connection and try again.'));
  });

  it('dismisses the notice when the user edits the name', async () => {
    renderSubjectForm({ notice: 'The subject changed just now.' });
    expect(screen.getByRole('status')).toHaveTextContent('The subject changed just now.');

    await userEvent.type(screen.getByRole('textbox', { name: 'Name' }), ' Marie');

    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });
});
