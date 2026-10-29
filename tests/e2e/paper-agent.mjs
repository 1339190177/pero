// Real Electron -> Rust Host -> Python App; Agent output is a labelled ACP fixture.
import { _electron as electron, expect } from '@playwright/test';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { createHash } from 'node:crypto';
import { setAppearance } from './sidebar-controls.mjs';

const root = resolve(import.meta.dirname, '../..');
const out = resolve(root, 'test-results/paper-agent');
await mkdir(out, { recursive: true });
await rm(resolve(out, 'failure.png'), { force: true });
const profile = await mkdtemp(resolve(tmpdir(), 'pero-paper-e2e-'));
const capture = resolve(profile, 'fixture-turns.jsonl');
const agents = resolve(profile, 'agents.json');
await writeFile(agents, JSON.stringify([{ id: 'paper-fixture', name: 'Synthetic Paper fixture', command: process.execPath,
  args: [resolve(root, 'tests/fixtures/paper-agent.mjs'), `--capture=${capture}`] }]));
const env = { ...process.env, PERO_DESKTOP_DATA_DIR: resolve(profile, 'desktop'), PERO_LOCALE: 'en',
  PERO_SETTINGS_PATH: resolve(profile, 'settings.json'), PERO_HISTORY_PATH: resolve(profile, 'sessions.jsonl'), PERO_AGENTS_FILE: agents };
delete env.ELECTRON_RUN_AS_NODE;
delete env.PERO_APPS_ROOT;
const launch = () => electron.launch({ args: [...(process.platform === 'darwin' ? ['--use-mock-keychain'] : []), resolve(root, 'desktop')], cwd: root, env });
let app;
const errors = [];
const checks = [];
const payloads = {};
const expectedConsole = [];
const fixtureTurns = async () => { try { return (await readFile(capture, 'utf8')).trim().split('\n').filter(Boolean).map(JSON.parse); } catch { return []; } };
const observe = page => {
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => {
    if (message.type() !== 'error') return;
    const url = message.location().url;
    if (/\b(?:400|403|503)\b/.test(message.text()) && /\/(?:api\/reports|host\/agent)$/.test(url)) expectedConsole.push({ url, text: message.text() });
    else errors.push(message.text());
  });
  page.on('response', response => {
    const asset = /^pero-app:\/\/paper-agent\/dist\/index\.(js|css)$/.exec(response.url())?.[1];
    if (asset) void response.body().then(body => { payloads[asset] = createHash('sha256').update(body).digest('hex'); }).catch(error => errors.push(error.message));
  });
};

async function openPaper(page) {
  await page.waitForFunction(() => window.peroDiagnostics && !document.getElementById('loading'), undefined, { timeout: 60000 });
  await expect.poll(async () => page.evaluate(() => JSON.parse(window.peroDiagnostics.snapshot()).apps?.some(item => item.name === 'paper-agent') ?? false)).toBe(true);
  await page.mouse.click(80, await page.evaluate(() => innerHeight - 76));
  await expect.poll(async () => page.evaluate(() => JSON.parse(window.peroDiagnostics.snapshot()).navigation)).toBe(7);
  await expect.poll(async () => page.evaluate(() => JSON.parse(window.peroDiagnostics.snapshot()).appsLoading)).toBe(false);
  await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
  await page.mouse.click(1305, 110);
  await expect.poll(async () => page.evaluate(() => JSON.parse(window.peroDiagnostics.snapshot()).activeApp?.name)).toBe('paper-agent');
  const frame = page.frameLocator('iframe[title="paper-agent"]');
  await expect(frame.getByRole('button', { name: 'Add papers', exact: true }).first()).toBeVisible({ timeout: 20000 });
  return frame;
}
const library = frame => frame.locator('body').evaluate(async () => (await (await fetch('/api/library')).json()));
const savedCount = async frame => (await library(frame)).reports.length;

try {
  app = await launch();
  let page = await app.firstWindow();
  observe(page);
  let frame = await openPaper(page);
  await expect(frame.getByRole('heading', { name: 'Start with a research question' })).toBeVisible();
  expect(await fixtureTurns()).toHaveLength(0);
  await page.screenshot({ path: resolve(out, 'empty.png') });
  await frame.getByRole('button', { name: 'Load synthetic example', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Synthetic reading · a reproducible citation example', exact: true })).toBeVisible();
  expect((await library(frame)).papers).toHaveLength(1);
  expect(await fixtureTurns()).toHaveLength(0);
  expect(await frame.locator('body').evaluate(() => typeof window.pero)).toBe('undefined');
  checks.push('actual installed Python backend, explicit synthetic import, immutable reading, no App preload or implicit Agent run');
  await page.screenshot({ path: resolve(out, 'source.png') });

  await expect(frame.getByLabel('Agent', { exact: true })).toContainText('Synthetic Paper fixture');
  await frame.getByLabel('Agent', { exact: true }).selectOption('paper-fixture');
  await frame.getByLabel('Research question', { exact: true }).fill('What does this synthetic source establish?');
  await frame.getByRole('button', { name: 'Start research', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Synthetic research report', exact: true })).toBeVisible({ timeout: 30000 });
  expect(await savedCount(frame)).toBe(1);
  checks.push('valid synthetic ACP output is validated and saved automatically through the Python backend');
  await frame.getByRole('button', { name: 'New research', exact: true }).click();
  await frame.getByLabel('Research question', { exact: true }).fill('paper-fixture-invalid: What does this synthetic source establish?');
  await frame.getByRole('button', { name: 'Start research', exact: true }).click();
  await expect(frame.getByText('The report was not saved. Correct the output below and validate again.', { exact: true })).toBeVisible({ timeout: 30000 });
  await expect(frame.getByRole('alert')).toContainText('Evidence quotation is absent');
  expect(await savedCount(frame)).toBe(1);
  const raw = frame.getByLabel('Report JSON', { exact: true });
  const output = JSON.parse(await raw.inputValue());
  await raw.fill('{broken JSON');
  await frame.getByRole('button', { name: 'Validate and save report', exact: true }).click();
  await expect(frame.getByRole('alert')).toContainText('No valid JSON report');
  expect(await savedCount(frame)).toBe(1);
  const paper = await frame.locator('body').evaluate(async () => {
    const catalog = await (await fetch('/api/library')).json();
    return (await fetch(`/api/papers/${catalog.papers[0].id}`)).json();
  });
  output.title = 'Corrected synthetic report';
  output.sections[0].evidence[0].quote = paper.content.split('\n').find(line => line.trim() && !line.startsWith('#'));
  await raw.fill(JSON.stringify(output));
  await frame.getByRole('button', { name: 'Validate and save report', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Corrected synthetic report', exact: true })).toBeVisible();
  expect(await savedCount(frame)).toBe(2);
  if (await frame.getByRole('button', { name: 'Dismiss error', exact: true }).count()) await frame.getByRole('button', { name: 'Dismiss error', exact: true }).click();
  checks.push('real Host-owned ACP fixture session, rejected invented quote, retained invalid output and successful explicit report repair');
  await page.screenshot({ path: resolve(out, 'report.png') });

  await frame.locator('.pa-citations button').first().click();
  await expect(frame.getByRole('complementary', { name: 'Citation evidence', exact: true }).locator('mark')).toHaveText(output.sections[0].evidence[0].quote);
  await page.screenshot({ path: resolve(out, 'evidence.png') });
  await frame.getByRole('complementary', { name: 'Citation evidence', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  await expect(frame.locator('.pa-citations button').first()).toBeFocused();
  checks.push('exact citation opens original frozen source context and closes with focus restoration');
  for (const [label, filename] of [['Export Markdown', 'research.md'], ['Export JSON', 'research.json']]) {
    const destination = resolve(out, filename);
    await rm(destination, { force: true });
    await app.evaluate(({ session }, path) => session.defaultSession.once('will-download', (_event, item) => item.setSavePath(path)), destination);
    await frame.getByRole('button', { name: label, exact: true }).click();
    await expect.poll(async () => { try { return (await readFile(destination, 'utf8')).length; } catch { return 0; } }).toBeGreaterThan(100);
  }
  expect(await readFile(resolve(out, 'research.md'), 'utf8')).toContain('Acceptance: unverified');
  expect(JSON.parse(await readFile(resolve(out, 'research.json'), 'utf8')).acceptance).toBe('unverified');
  checks.push('native Markdown/JSON downloads retain source evidence and unverified acceptance');
  await page.emulateMedia({ media: 'print' });
  await expect(frame.locator('.pa-print-appendix')).toBeVisible();
  await expect(frame.locator('.pa-print-appendix')).toContainText(paper.url);
  await expect(frame.locator('.pa-print-appendix')).toContainText(paper.contentDigest);
  await expect(frame.locator('.pa-printed-quote').first()).toContainText(output.sections[0].evidence[0].quote);
  await page.emulateMedia({ media: 'screen' });
  checks.push('print-media report retains source URLs, snapshot digests and exact evidence; native print dialog remains untested');

  await frame.getByRole('button', { name: 'New research', exact: true }).click();
  await frame.getByLabel('Research question', { exact: true }).fill('paper-fixture-wait: cancellation acceptance');
  await frame.getByRole('button', { name: 'Start research', exact: true }).click();
  await expect(frame.getByRole('button', { name: 'Cancel research', exact: true })).toBeVisible();
  await frame.getByRole('button', { name: 'Cancel research', exact: true }).click();
  await expect(frame.getByText('Research cancelled', { exact: true })).toBeVisible({ timeout: 15000 });
  expect(await savedCount(frame)).toBe(2);
  checks.push('Host-owned cancellation ends waiting fixture without saving a report');
  await page.screenshot({ path: resolve(out, 'cancelled.png') });

  await frame.getByRole('tab', { name: /^Reports/ }).click();
  await frame.getByRole('button', { name: 'Corrected synthetic report', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Corrected synthetic report', exact: true })).toBeVisible();
  await setAppearance(page, 'dark');
  await expect.poll(async () => frame.locator('html').evaluate(element => element.style.colorScheme)).toBe('dark');
  await app.evaluate(({ Menu }) => Menu.getApplicationMenu().getMenuItemById('locale-zh-CN').click());
  await expect(frame.getByRole('button', { name: '添加论文', exact: true }).first()).toBeVisible();
  await page.screenshot({ path: resolve(out, 'dark-zh.png') });
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1000, 800));
  await expect.poll(async () => page.locator('#pero-app-surface').evaluate(element => Math.round(element.getBoundingClientRect().width))).toBe(760);
  await expect.poll(async () => frame.locator('html').evaluate(element => element.clientWidth)).toBe(760);
  await expect.poll(async () => frame.locator('html').evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  await page.screenshot({ path: resolve(out, 'narrow.png') });
  checks.push('live dark theme, Chinese locale and narrow window without horizontal document overflow');

  const beforeRestart = (await fixtureTurns()).length;
  await app.close(); app = undefined;
  app = await launch(); page = await app.firstWindow();
  observe(page);
  await page.waitForFunction(() => window.peroDiagnostics && !document.getElementById('loading'), undefined, { timeout: 60000 });
  await app.evaluate(({ Menu, BrowserWindow }) => {
    Menu.getApplicationMenu().getMenuItemById('locale-en').click();
    BrowserWindow.getAllWindows()[0].setSize(1488, 1086);
  });
  frame = await openPaper(page);
  await expect.poll(async () => savedCount(frame)).toBe(2);
  await frame.getByRole('tab', { name: /^Reports/ }).click();
  await frame.getByRole('button', { name: 'Corrected synthetic report', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Corrected synthetic report', exact: true })).toBeVisible();
  expect((await fixtureTurns()).length).toBe(beforeRestart);
  checks.push('saved sources, frozen inputs and report reopen after Desktop restart without re-running Agent');

  await page.evaluate(() => window.pero.appsCommand({ action: 'apps_open', name: 'virtual-cell', panel: 'main' }));
  const other = page.frameLocator('iframe[title="virtual-cell"]');
  await expect(other.locator('body')).toBeAttached();
  const denied = await other.locator('body').evaluate(async () => {
    const response = await fetch('/host/agent', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ operation: 'info' }) });
    return { status: response.status, text: await response.text() };
  });
  expect(denied.status).toBeGreaterThanOrEqual(400);
  expect(denied.text).toMatch(/permission|grant|Agent|agent/i);
  checks.push('App without Agent grant is refused at the real Host boundary');
  await expect.poll(() => Object.keys(payloads).length).toBe(2);
  for (const extension of ['js', 'css']) expect(payloads[extension]).toBe(createHash('sha256').update(await readFile(resolve(root, `apps/paper-agent/dist/index.${extension}`))).digest('hex'));
  expect(errors).toEqual([]);
  await writeFile(resolve(out, 'result.json'), JSON.stringify({ fixtureOnly: true, realModel: false, payloads, checks, errors, expectedConsole }, null, 2));
  console.log(JSON.stringify({ fixtureOnly: true, realModel: false, payloads, checks, errors, expectedConsole }));
} catch (error) {
  if (app) {
    const page = await app.firstWindow();
    await page.screenshot({ path: resolve(out, 'failure.png') }).catch(() => {});
    console.error('Paper Agent failure state', { errors, state: await page.evaluate(() => {
      const value = JSON.parse(window.peroDiagnostics?.snapshot() ?? '{}');
      return { navigation: value.navigation, activeApp: value.activeApp, appError: value.appError,
        chats: value.chats?.map(chat => ({ id: chat.id, state: chat.state, agent: chat.agent })) };
    }).catch(() => 'unavailable') });
  }
  throw error;
} finally {
  if (app) await app.close();
  await rm(profile, { recursive: true, force: true });
}
