import { createRoot } from 'react-dom/client';
import { afterEach, expect, it } from 'vitest';
import { Heading, ScreenTransitionContext, SecondaryScreenContext } from './Heading';

const actGlobal = globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean };
const previousAct = actGlobal.IS_REACT_ACT_ENVIRONMENT;

afterEach(() => {
  actGlobal.IS_REACT_ACT_ENVIRONMENT = previousAct;
  document.body.replaceChildren();
});

async function activeElementWhenHeadingAppears(transitioned: boolean) {
  // Outside act(), like a real browser: React defers passive effects past the commit, and the
  // MutationObserver records focus at the moment the heading is inserted, before those effects run.
  actGlobal.IS_REACT_ACT_ENVIRONMENT = false;
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const seen = new Promise<{ heading: Element; active: Element | null }>((resolve) => {
    const observer = new MutationObserver(() => {
      const heading = container.querySelector('h1');
      if (!heading) return;
      observer.disconnect();
      resolve({ heading, active: document.activeElement });
    });
    observer.observe(container, { childList: true, subtree: true });
  });
  root.render(
    <ScreenTransitionContext value={{ current: transitioned }}>
      <Heading>Screen</Heading>
    </ScreenTransitionContext>,
  );
  const result = await seen;
  root.unmount();
  return result;
}

it('after a screen transition the heading already has focus when it appears in the DOM', async () => {
  const { heading, active } = await activeElementWhenHeadingAppears(true);
  expect(active).toBe(heading);
});

it('on the first screen the heading appears without taking focus', async () => {
  const { active } = await activeElementWhenHeadingAppears(false);
  expect(active).toBe(document.body);
});

it('names the document after the current heading and resets the title when the heading goes', async () => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const { act } = await import('react');
  actGlobal.IS_REACT_ACT_ENVIRONMENT = true;
  document.title = 'Tendo';
  await act(async () => root.render(<Heading>All items</Heading>));
  expect(document.title).toBe('All items — Tendo');
  await act(async () => root.render(<Heading>Renew passport soon</Heading>));
  expect(document.title).toBe('Renew passport soon — Tendo');
  await act(async () => root.render(<p>Loading…</p>));
  expect(document.title).toBe('Tendo');
  await act(async () => root.unmount());
});

it('a secondary screen beside the current one yields the h1, focus and document title', async () => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const { act } = await import('react');
  actGlobal.IS_REACT_ACT_ENVIRONMENT = true;
  document.title = 'Tendo';
  await act(async () => root.render(
    <ScreenTransitionContext value={{ current: true }}>
      <SecondaryScreenContext value={true}><Heading>Household list</Heading></SecondaryScreenContext>
      <Heading>Selected item</Heading>
    </ScreenTransitionContext>,
  ));
  expect(container.querySelectorAll('h1')).toHaveLength(1);
  expect(container.querySelector('h1')).toHaveTextContent('Selected item');
  expect(container.querySelector('h2')).toHaveTextContent('Household list');
  expect(document.activeElement).toBe(container.querySelector('h1'));
  expect(document.title).toBe('Selected item — Tendo');
  await act(async () => root.unmount());
});
