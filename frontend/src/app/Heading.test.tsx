import { createRoot } from 'react-dom/client';
import { afterEach, expect, it } from 'vitest';
import { Heading, ScreenTransitionContext } from './Heading';

const actGlobal = globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean };
const previousAct = actGlobal.IS_REACT_ACT_ENVIRONMENT;

afterEach(() => {
  actGlobal.IS_REACT_ACT_ENVIRONMENT = previousAct;
  document.body.replaceChildren();
});

// Render work longer than React's 5 ms scheduler frame makes React yield before deferred (passive) effects.
function SlowSibling() {
  const end = performance.now() + 25;
  while (performance.now() < end) { /* busy wait */ }
  return null;
}

async function activeElementWhenHeadingAppears(transitioned: boolean) {
  // Outside act(), like a real browser and like Testing Library's findBy* polling.
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
      <SlowSibling />
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
