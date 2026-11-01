import { fireEvent, render, screen } from '@testing-library/react';
import type { FormEvent } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { Button } from './button';
import { Dialog } from './dialog';
import { Disclosure } from './disclosure';
import { DoneControl } from './list-row';
import { Pagination } from './pagination';
import { Switch } from './switch';
import { Field } from './field';
import { Input } from './input';
import { Select } from './select';
import { Textarea } from './textarea';
import { Skeleton } from './skeleton';
import { EmptyState } from './empty-state';
import { GroupHeader } from './group-header';
import { Notice } from './notice';

Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.open = true; } });
Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.open = false; } });

describe('UI primitive behavior', () => {
  it('dialog uses native modal activation, Escape handling, focus return, and busy dismissal lock', () => {
    const showModal = vi.spyOn(HTMLDialogElement.prototype, 'showModal');
    const onOpenChange = vi.fn();
    const trigger = document.createElement('button');
    document.body.append(trigger);
    const { rerender, unmount } = render(<Dialog open title="Edit item" closeLabel="Close" onOpenChange={onOpenChange} triggerRef={{ current: trigger }}>Form</Dialog>);
    expect(showModal).toHaveBeenCalledTimes(1);
    const dialog = screen.getByRole('dialog', { name: 'Edit item' });
    expect(dialog.getAttribute('aria-labelledby')).not.toBe('dialog-title');
    expect(screen.getByRole('button', { name: 'Close' })).toBeVisible();
    expect(dialog.className).toContain('max-w-xl');
    fireEvent(dialog, new Event('cancel', { cancelable: true }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    rerender(<Dialog open title="Edit item" closeLabel="Close" busy onOpenChange={onOpenChange} triggerRef={{ current: trigger }}>Form</Dialog>);
    fireEvent(dialog, new Event('cancel', { cancelable: true }));
    expect(onOpenChange).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: 'Close' })).toBeDisabled();
    const showCallsBeforeUnexpectedClose = showModal.mock.calls.length;
    fireEvent(dialog, new Event('close'));
    expect(showModal).toHaveBeenCalledTimes(showCallsBeforeUnexpectedClose + 1);
    rerender(<Dialog open={false} title="Edit item" closeLabel="Close" onOpenChange={onOpenChange} triggerRef={{ current: trigger }}>Form</Dialog>);
    expect(trigger).toHaveFocus();
    unmount();
    trigger.remove();
    showModal.mockRestore();
    vi.restoreAllMocks();
  });
  it('dialogs have unique heading ids and sheet classes replace desktop sizing', () => {
    const { container } = render(<><Dialog open title="First" closeLabel="Close first" sheet onOpenChange={() => undefined}>A</Dialog><Dialog open title="Second" closeLabel="Close second" onOpenChange={() => undefined}>B</Dialog></>);
    const dialogs = [...container.querySelectorAll('dialog')];
    const ids = dialogs.map((dialog) => dialog.getAttribute('aria-labelledby'));
    expect(new Set(ids).size).toBe(2);
    expect(dialogs[0].className).toContain('w-full');
    expect(dialogs[0].className).not.toContain('max-w-xl rounded-2xl');
  });
  it('dialog returns focus to fallback heading when trigger is absent', () => {
    const { rerender } = render(<Dialog open title="Fallback" closeLabel="Close" onOpenChange={() => undefined}>Body</Dialog>);
    const heading = screen.getByRole('heading', { name: 'Fallback' });
    rerender(<Dialog open={false} title="Fallback" closeLabel="Close" onOpenChange={() => undefined}>Body</Dialog>);
    expect(heading).toHaveFocus();
  });
  it('button has visible keyboard focus styling and supports disabled and busy states', () => {
    render(<Button disabled aria-busy="true">Save</Button>);
    const button = screen.getByRole('button', { name: 'Save' });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('aria-busy', 'true');
    expect(button.className).toContain('focus-visible:outline-focus');
    expect(button).toHaveAttribute('type', 'button');
  });
  it('disclosure wires expanded state and controlled content', () => {
    render(<Disclosure title="More details">Details</Disclosure>);
    const trigger = screen.getByRole('button', { name: 'More details' });
    const content = screen.getByText('Details').parentElement!;
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(document.getElementById(trigger.getAttribute('aria-controls')!)).toContainElement(screen.getByText('Details'));
    expect(content).toBeTruthy();
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(content).toBeVisible();
  });
  it('switch exposes checked state and changes on activation', () => {
    const onChange = vi.fn();
    render(<Switch aria-label="Repeat" checked={false} onCheckedChange={onChange} />);
    const toggle = screen.getByRole('switch', { name: 'Repeat' });
    expect(toggle).toHaveAttribute('aria-checked', 'false');
    fireEvent.click(toggle);
    expect(onChange).toHaveBeenCalledWith(true);
  });
  it('done control has an accessible name, touch target, and pressed state', () => {
    const { rerender } = render(<DoneControl label="Mark Clean oven done" done={false} onToggle={() => undefined} />);
    const done = screen.getByRole('button', { name: 'Mark Clean oven done' });
    expect(done).toHaveAttribute('aria-pressed', 'false');
    expect(done.className).toContain('w-11');
    expect(done.className).toContain('h-11');
    rerender(<DoneControl label="Mark Clean oven done" done onToggle={() => undefined} />);
    expect(screen.getByRole('button', { name: 'Mark Clean oven done' }).querySelector('[aria-hidden="true"]')).toHaveTextContent('✓');
  });
  it('pagination names cursor controls and range, and disables both bounds', () => {
    const previous = vi.fn();
    const next = vi.fn();
    const { rerender } = render(<Pagination canPrevious={false} canNext range="Items 1–25 of 80" label="Item pages" previousLabel="Previous" nextLabel="Next" onPrevious={previous} onNext={next} />);
    expect(screen.getByRole('navigation', { name: 'Item pages' })).toHaveTextContent('Items 1–25 of 80');
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled();
    expect(screen.getByRole('navigation', { name: 'Item pages' })).toHaveAccessibleName('Item pages');
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(next).toHaveBeenCalledOnce();
    rerender(<Pagination canPrevious canNext={false} range="Items 26–50 of 80" label="Item pages" previousLabel="Previous" nextLabel="Next" onPrevious={previous} onNext={next} />);
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
    rerender(<Pagination canPrevious canNext={false} hideNext range="Items 26–50 of 50" label="Item pages" previousLabel="Previous" nextLabel="Next" onPrevious={previous} onNext={next} />);
    expect(screen.queryByRole('button', { name: 'Next' })).not.toBeInTheDocument();
  });
  it('disclosure inside a form does not submit', () => {
    const submit = vi.fn((event: FormEvent<HTMLFormElement>) => event.preventDefault());
    render(<form onSubmit={submit}><Disclosure title="More">Details</Disclosure></form>);
    fireEvent.click(screen.getByRole('button', { name: 'More' }));
    expect(submit).not.toHaveBeenCalled();
  });
  it('field wires label, control, hint and error description', () => {
    render(<Field id="username" label="Username" hint="Used to sign in" error="Required">{(props) => <Input {...props} />}</Field>);
    const input = screen.getByRole('textbox', { name: 'Username' });
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input.getAttribute('aria-describedby')).toContain('username-hint');
    expect(input.getAttribute('aria-describedby')).toContain('username-error');
  });
  it('field supports select and textarea controls with invalid description wiring', () => {
    const { rerender } = render(<Field id="choice" label="Choice" error="Choose one">{(props) => <Select {...props}><option>One</option></Select>}</Field>);
    expect(screen.getByRole('combobox')).toHaveAttribute('aria-describedby', 'choice-error');
    rerender(<Field id="notes" label="Notes" error="Required">{(props) => <Textarea {...props} />}</Field>);
    expect(screen.getByRole('textbox', { name: 'Notes' })).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('textbox', { name: 'Notes' })).toHaveAttribute('aria-describedby', 'notes-error');
  });
  it('switch respects disabled state', () => {
    render(<Switch aria-label="Repeat" checked={false} disabled onCheckedChange={() => undefined} />);
    expect(screen.getByRole('switch', { name: 'Repeat' })).toBeDisabled();
  });
  it('skeleton is hidden from assistive technology', () => {
    render(<Skeleton data-testid="loading-skeleton" />);
    expect(screen.getByTestId('loading-skeleton')).toHaveAttribute('aria-hidden', 'true');
  });
  it('empty state has a heading and group header displays count', () => {
    render(<><EmptyState title="Nothing here" /><GroupHeader count={4}>Needs attention</GroupHeader></>);
    expect(screen.getByRole('heading', { name: 'Nothing here' })).toBeVisible();
    expect(screen.getByRole('heading', { name: /^Needs attention$/ })).toBeVisible();
    expect(screen.getByText('4')).toBeVisible();
  });
  it('notice maps tones to alert/status roles', () => {
    const { rerender } = render(<Notice tone="error">Try again</Notice>);
    expect(screen.getByRole('alert')).toHaveTextContent('Try again');
    rerender(<Notice tone="success">Saved</Notice>);
    expect(screen.getByRole('status')).toHaveTextContent('Saved');
  });
});
