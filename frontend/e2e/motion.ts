import type { Page } from '@playwright/test';

/**
 * Parses one CSS time value (`ms` or `s`, as serialized by getComputedStyle). The CSS `infinite`
 * keyword maps to infinity. Anything else throws, so an unexpected serialization fails the check
 * instead of silently counting as no motion.
 */
export function parseCssTime(value: string): number {
  const input = value.trim().toLowerCase();
  if (input === 'infinite') return Number.POSITIVE_INFINITY;
  const match = input.match(/^(-?(?:\d+\.?\d*|\.\d+)(?:e[+-]?\d+)?)(ms|s)$/);
  if (!match) throw new Error(`unparseable CSS time: ${JSON.stringify(value)}`);
  const amount = Number(match[1]);
  return match[2] === 's' ? amount * 1000 : amount;
}

/**
 * Returns the largest duration+delay pair of two comma-separated CSS time lists. Per CSS list
 * matching, the shorter list repeats; the longer list sets the count.
 */
export function longestCssTimeSum(durations: string, delays: string): number {
  const durationList = durations.split(',').map(parseCssTime);
  const delayList = delays.split(',').map(parseCssTime);
  const count = Math.max(durationList.length, delayList.length);
  let longest = 0;
  for (let index = 0; index < count; index += 1) {
    longest = Math.max(longest, durationList[index % durationList.length] + delayList[index % delayList.length]);
  }
  return longest;
}

export type MotionMeasurement = {
  elements: number;
  longestMs: number;
  offender: { tag: string; class: string; pseudo: string; text: string } | null;
  animations: number;
  animationTimings: Array<{ durationMs: number; delayMs: number; endDelayMs: number; iterations: number; totalMs: number }>;
  longestAnimationMs: number;
  rootScrollBehavior: string;
};

type RawStyle = { transitionDuration: string; transitionDelay: string; animationDuration: string; animationDelay: string };
type RawSample = RawStyle & { tag: string; class: string; pseudo: string; text: string };

/**
 * Measures every element of the document and its ::before/::after: computed transition and
 * animation duration plus delay, active Web Animations timings, and root scroll behavior. The
 * browser only collects raw computed strings (deduplicated); parsing happens here with the
 * unit-tested parser.
 */
export async function measureMotion(page: Page): Promise<MotionMeasurement> {
  const raw = await page.evaluate(() => {
    const samples = new Map<string, RawSample>();
    let elements = 0;
    for (const element of document.querySelectorAll('*')) {
      elements += 1;
      for (const pseudo of [null, '::before', '::after'] as const) {
        const style = getComputedStyle(element, pseudo);
        const values = { transitionDuration: style.transitionDuration, transitionDelay: style.transitionDelay, animationDuration: style.animationDuration, animationDelay: style.animationDelay };
        const key = JSON.stringify(values);
        if (!samples.has(key)) {
          samples.set(key, {
            ...values,
            tag: element.tagName.toLowerCase(),
            class: typeof element.className === 'string' ? element.className : '',
            pseudo: pseudo ?? '',
            text: (element.textContent ?? '').trim().replace(/\s+/g, ' ').slice(0, 80),
          });
        }
      }
    }
    const animations = document.getAnimations().map((animation) => {
      const timing = animation.effect?.getComputedTiming();
      return {
        duration: timing?.duration === undefined ? null : String(timing.duration),
        delay: timing?.delay ?? null,
        endDelay: timing?.endDelay ?? null,
        iterations: timing?.iterations ?? null,
      };
    });
    return { elements, samples: [...samples.values()], animations, rootScrollBehavior: getComputedStyle(document.documentElement).scrollBehavior };
  });

  let longestMs = 0;
  let offender: MotionMeasurement['offender'] = null;
  for (const sample of raw.samples) {
    const motion = Math.max(
      longestCssTimeSum(sample.transitionDuration, sample.transitionDelay),
      longestCssTimeSum(sample.animationDuration, sample.animationDelay),
    );
    if (motion > longestMs || offender === null) {
      if (motion > longestMs) longestMs = motion;
      offender = { tag: sample.tag, class: sample.class, pseudo: sample.pseudo, text: sample.text };
    }
  }
  const number = (label: string, value: unknown) => {
    const parsed = typeof value === 'number' ? value : Number(value);
    if (value === null || Number.isNaN(parsed)) throw new Error(`unmeasurable animation ${label}: ${JSON.stringify(value)}`);
    return parsed;
  };
  const animationTimings = raw.animations.map((animation) => {
    const durationMs = number('duration', animation.duration);
    const delayMs = number('delay', animation.delay);
    const endDelayMs = number('endDelay', animation.endDelay);
    const iterations = number('iterations', animation.iterations);
    const totalMs = Number.isFinite(iterations) ? durationMs * iterations + delayMs + endDelayMs : Number.POSITIVE_INFINITY;
    return { durationMs, delayMs, endDelayMs, iterations, totalMs };
  });
  const longestAnimationMs = Math.max(0, ...animationTimings.map((entry) => entry.totalMs));
  return {
    elements: raw.elements,
    longestMs: Math.max(longestMs, longestAnimationMs),
    offender,
    animations: animationTimings.length,
    animationTimings,
    longestAnimationMs,
    rootScrollBehavior: raw.rootScrollBehavior,
  };
}
