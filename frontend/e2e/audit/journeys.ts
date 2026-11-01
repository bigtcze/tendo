import { expect, type Browser, type Page } from '@playwright/test';
import { en } from '../../src/i18n/en';

export interface JourneyResult {
  viewport: string;
  steps: number;
  interactions: number;
  keystrokes: number;
  wallMs: number;
  obstacles: string[];
}

export class JourneyRecorder {
  private startedAt = 0;
  private lastActionAt = 0;
  private steps = 0;
  private interactions = 0;
  private keystrokes = 0;
  private readonly obstacles: string[] = [];

  get started(): boolean { return this.startedAt > 0; }
  start(): void { this.startedAt = performance.now(); }
  step(): void { this.steps += 1; this.lastActionAt = performance.now(); }
  interaction(keystrokes = 0): void { this.interactions += 1; this.keystrokes += keystrokes; this.lastActionAt = performance.now(); }
  obstacle(value: string): void { this.obstacles.push(value); }

  result(viewport: string): JourneyResult {
    const end = this.lastActionAt || performance.now();
    return {
      viewport,
      steps: this.steps,
      interactions: this.interactions,
      keystrokes: this.keystrokes,
      wallMs: Math.round(end - this.startedAt),
      obstacles: this.obstacles,
    };
  }
}

export async function runFirstRunSetup(page: Page, setupToken: string, ownerPassword: string, recorder = new JourneyRecorder()): Promise<JourneyResult> {
  if (!recorder.started) recorder.start();
  await page.goto('/'); recorder.step();
  await page.locator('#onboarding-timezone').selectOption('Europe/Prague'); recorder.interaction();
  const code = page.getByLabel(en['onboarding.setupCode'], { exact: true }); await code.fill(setupToken); recorder.interaction(setupToken.length);
  const household = page.getByLabel(en['onboarding.householdName']); await household.fill('Audit household'); recorder.interaction('Audit household'.length);
  const login = page.locator('#onboarding-login'); await login.fill('audit_owner'); recorder.interaction('audit_owner'.length);
  const password = page.locator('#onboarding-password'); await password.fill(ownerPassword); recorder.interaction(ownerPassword.length);
  recorder.step();
  await page.getByRole('button', { name: en['onboarding.submit'] }).click(); recorder.interaction();
  await expect(page.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
  return recorder.result('390x844');
}

export async function runHouseholdJourneys(page: Page, browser: Browser, householdId: string, ownerPassword: string): Promise<Record<string, JourneyResult>> {
  const results: Record<string, JourneyResult> = {};
  const record = async (id: string, run: (recorder: JourneyRecorder) => Promise<void>, width = 390) => {
    await page.setViewportSize({ width, height: 844 });
    const recorder = new JourneyRecorder(); recorder.start();
    await run(recorder);
    results[`${id} ${width}x844`] = recorder.result(`${width}x844`);
  };
  const requireItems = async (): Promise<void> => {
    await expect.poll(() => page.locator('main li[data-item-id]').count(), { timeout: 30_000 }).toBeGreaterThan(0);
  };
  const click = async (locator: ReturnType<Page['getByRole']>, recorder: JourneyRecorder) => { await locator.click(); recorder.interaction(); };
  const fill = async (locator: ReturnType<Page['getByLabel']>, value: string, recorder: JourneyRecorder) => {
    await locator.fill(value);
    recorder.interaction(value.length);
  };

  const j1 = async (recorder: JourneyRecorder) => {
    await page.goto('/'); await requireItems(); recorder.step();
    await click(page.getByRole('button', { name: en['items.add'], exact: true }), recorder);
    const form = page.getByRole('form', { name: en['items.add.formLabel'] });
    await fill(form.getByLabel(en['items.title']), 'Journey one-off inspection', recorder);
    await form.getByLabel(en['items.subject'], { exact: true }).selectOption({ index: 1 }); recorder.interaction();
    recorder.step();
    await click(form.getByRole('button', { name: en['items.add.submit'] }), recorder);
    await expect(page.getByRole('main').getByText('Journey one-off inspection')).toBeVisible();
  };
  await record('J1 add one-off from Home', j1);
  await record('J1 add one-off from Home', j1, 1440);

  const j2 = async (recorder: JourneyRecorder) => {
    await page.goto('/'); await requireItems(); recorder.step();
    for (const [title, fluid] of [['Journey fixed cadence', false], ['Journey completion cadence', true]] as const) {
      await click(page.getByRole('button', { name: en['items.add'], exact: true }), recorder);
      const form = page.getByRole('form', { name: en['items.add.formLabel'] });
      await fill(form.getByLabel(en['items.title']), title, recorder);
      await form.getByLabel(en['items.subject'], { exact: true }).selectOption({ index: 1 }); recorder.interaction();
      const repeat = form.getByRole('switch', { name: en['items.repeat'], exact: true });
      await repeat.check(); recorder.interaction();
      await form.getByRole('spinbutton').fill('2'); recorder.interaction(1);
      await form.getByRole('combobox', { name: en['items.intervalUnit'] }).selectOption('week'); recorder.interaction();
      if (fluid) { await form.getByRole('switch', { name: en['items.fluid'] }).check(); recorder.interaction(); }
      recorder.step();
      await click(form.getByRole('button', { name: en['items.add.submit'] }), recorder);
      await expect(page.getByRole('main').getByText(title)).toBeVisible();
    }
    recorder.obstacle('Repeat reveals interval and cadence controls.');
  };
  await record('J2 fixed and after-completion recurring adds', j2);

  await record('J3 find Needs attention by keyboard', async recorder => {
    await page.goto('/'); await requireItems(); recorder.step();
    await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); });
    const attentionHeading = page.getByRole('heading', { level: 2, name: /Needs attention/ });
    const firstAttentionRow = page.locator('main section').filter({ has: attentionHeading }).locator('li[data-item-id]').first();
    const titleLink = firstAttentionRow.locator('a').first();
    await expect(titleLink).toBeVisible();
    let tabs = 0;
    for (; tabs < 100; tabs++) {
      await page.keyboard.press('Tab'); recorder.interaction();
      if (await titleLink.evaluate((node) => node === document.activeElement)) break;
    }
    expect(tabs).toBeLessThan(100);
    recorder.step(); recorder.obstacle(`First Needs attention row required ${tabs + 1} Tab keypresses from page start.`);
  });

  const j4 = async (recorder: JourneyRecorder) => {
    await page.goto('/'); await requireItems(); recorder.step();
    const done = page.locator('main li[data-item-id]').first().locator('button[data-action="done"]');
    await expect(done).toBeVisible();
    await click(done, recorder);
    const undo = page.getByRole('button', { name: en['items.undo'], exact: true });
    await expect(undo).toBeVisible(); recorder.step(); await click(undo, recorder);
  };
  await record('J4 mark done and undo from Home', j4);
  await record('J4 mark done and undo from Home', j4, 1440);

  const j5 = async (recorder: JourneyRecorder) => {
    const doneList = await page.request.get(`/api/v1/households/${householdId}/items?limit=100&done=true&archived=false`);
    const doneData = await doneList.json() as { items: Array<{ title: string }> };
    const oldTitle = doneData.items.at(-1)?.title;
    expect(oldTitle).toBeTruthy();
    await page.goto('/items');
    await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
    await expect.poll(() => page.locator('main li[data-item-id]').count(), { timeout: 30_000 }).toBeGreaterThan(0);
    recorder.step();
    await page.getByLabel(en['allItems.status.label']).selectOption('done'); recorder.interaction();
    const search = page.getByRole('searchbox', { name: en['allItems.search'], exact: true });
    await fill(search, oldTitle!, recorder);
    await click(page.getByRole('button', { name: en['allItems.searchButton'], exact: true }), recorder);
    const result = page.getByRole('main').getByRole('link', { name: oldTitle!, exact: true }).first();
    await expect(result).toBeVisible(); recorder.step(); await click(result, recorder);
  };
  await record('J5 search an old done item in All items', j5);
  await record('J5 search an old done item in All items', j5, 1440);

  const j6 = async (recorder: JourneyRecorder) => {
    await page.goto('/'); await requireItems();
    const row = page.locator('main li[data-item-id]').first();
    const href = await row.locator('a').first().getAttribute('href');
    expect(href).toBeTruthy(); await page.goto(href!); recorder.step();
    await click(page.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }), recorder);
    const form = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
    const title = form.getByLabel(en['items.title']);
    await fill(title, 'Journey edit then restore item', recorder);
    await click(form.getByRole('button', { name: en['itemDetail.save'], exact: true }), recorder);
    await expect(page.getByRole('heading', { name: 'Journey edit then restore item' })).toBeVisible();
    recorder.step();
    const archive = page.getByRole('button', { name: en['subjects.archive'], exact: true });
    await click(archive, recorder);
    await expect(page.getByRole('button', { name: en['subjects.restore'], exact: true })).toBeVisible();
    await click(page.getByRole('button', { name: en['subjects.restore'], exact: true }), recorder);
    recorder.obstacle('Detail has no Done action; archive and restore are available there.');
  };
  await record('J6 edit archive restore from detail', j6);

  await record('J7 invite a household member', async recorder => {
    await page.goto('/members'); recorder.step();
    await click(page.getByRole('button', { name: en['members.invite.action'], exact: true }), recorder);
    await expect(page.getByRole('textbox', { name: en['members.invite.link'] })).toBeVisible();
    await click(page.getByRole('button', { name: en['members.invite.copy'], exact: true }), recorder);
    recorder.step(); recorder.obstacle('Invite creation displays a one-time link and Copy control.');
  });

  const loginContext = await browser.newContext({ baseURL: process.env.E2E_BASE_URL!, reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  try {
    const loginPage = await loginContext.newPage();
    const loginRecorder = new JourneyRecorder(); loginRecorder.start();
    await loginPage.goto('/login');
    await expect(loginPage.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
    loginRecorder.step();
    await loginPage.getByLabel(en['login.login'], { exact: true }).fill('audit_owner'); loginRecorder.interaction('audit_owner'.length);
    await loginPage.getByLabel(en['login.password'], { exact: true }).fill(ownerPassword); loginRecorder.interaction(ownerPassword.length);
    await loginPage.getByRole('button', { name: en['login.submit'], exact: true }).click(); loginRecorder.interaction();
    await expect(loginPage.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
    loginRecorder.step(); results['J8 sign in from /login 390x844'] = loginRecorder.result('390x844');
  } finally { await loginContext.close(); }

  return results;
}
