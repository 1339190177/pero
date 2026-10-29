import { exerciseLocalSearch } from './paper-search-flow.mjs';
// Real Electron -> Rust Host -> Python App; Agent output is a labelled ACP fixture.
import { expect } from '@playwright/test';
import { electron } from './electron.mjs';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { exercisePdf, reopenPdf, setPaperAppearance as setAppearance } from './paper-pdf-flow.mjs';

const root = resolve(import.meta.dirname, '../..');
const out = resolve(root, 'test-results/paper-agent');
await mkdir(out, { recursive: true });
await rm(resolve(out, 'failure.png'), { force: true });
const profile = await mkdtemp(resolve(tmpdir(), 'pero-paper-e2e-'));
const pdfPath = resolve(profile, 'source.pdf');
const pdfFixture = JSON.parse(execFileSync(process.env.PERO_APP_PYTHON || 'python3', ['-c', `
import runpy,sys,json
from pathlib import Path
from contextlib import closing
import pypdfium2 as pdfium
fixture=runpy.run_path(sys.argv[1])
real=bool(sys.argv[3])
data=Path(sys.argv[3]).read_bytes() if real else fixture['pdf_fixture']()
Path(sys.argv[2]).write_bytes(data)
with closing(pdfium.PdfDocument(data)) as pdf, closing(pdf[0]) as page, closing(page.get_textpage()) as text:
 quote='Attention Is All You Need' if real else 'Exact original evidence.'
 with closing(text.search(quote)) as search:
  start,count=search.get_next()
 a=text.get_charbox(start); b=text.get_charbox(start+count-1)
 width,height=page.get_size()
 print(json.dumps(dict(realPdf=real,pages=len(pdf),quote=quote,text=text.get_text_range(),selection=dict(x1=(a[0]+1)/width,x2=(b[2]-1)/width,y=(height-(a[1]+a[3])/2)/height))))
`, resolve(root, 'apps/paper-agent/test/test_pdf.py'), pdfPath, process.env.PERO_PAPER_PDF_FILE || ''], { encoding: 'utf8' }));
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
    if (/\b(?:400|403|503)\b/.test(message.text()) && /\/(?:api\/(?:reports|discover|pdf|library\/search)|host\/agent)$/.test(url)) expectedConsole.push({ url, text: message.text() });
    else errors.push({ url, text: message.text() });
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
  // Change only the disposable installed package: keep the real Python transport,
  // adapter and store while replacing the external HTTP seam with synthetic data.
  await page.waitForFunction(() => window.peroDiagnostics && JSON.parse(window.peroDiagnostics.snapshot()).apps?.some(item => item.name === 'paper-agent'));
  const installedBackend = resolve(profile, 'desktop/apps/paper-agent/backend');
  await writeFile(resolve(installedBackend, 'discovery_fixture.py'), await readFile(resolve(root, 'tests/fixtures/paper-discovery.py')));
  const serverPath = resolve(installedBackend, 'server.py');
  await writeFile(serverPath, (await readFile(serverPath, 'utf8')).replace('from paper_agent.http import handler', 'from paper_agent.http import handler\nfrom paper_agent.literature import LiteratureClient\nfrom discovery_fixture import fetch').replace('os.environ["PERO_APP_TOKEN"]))', 'os.environ["PERO_APP_TOKEN"], LiteratureClient(fetch), pdf_files=PdfFiles(lambda *_args, **_kwargs: BytesIO(Path(' + JSON.stringify(pdfPath) + ').read_bytes()))))').replace('from paper_agent.http import handler\n', 'from paper_agent.http import handler\nfrom paper_agent.pdf_files import PdfFiles\nfrom io import BytesIO\nfrom pathlib import Path\n'));
  let frame = await openPaper(page);
  if (process.env.PERO_PAPER_PDF_ONLY === '1') {
    const savedPdf = await exercisePdf({ frame, page, app, out, fixture: pdfFixture });
    await app.close(); app = await launch(); page = await app.firstWindow(); observe(page); frame = await openPaper(page);
    await reopenPdf({ frame, saved: savedPdf });
    checks.push('PDF rendering, pointer selection, crop, matrix, navigation and restart');
  } else {
  await expect(frame.getByRole('heading', { name: 'Add a paper to start reading' })).toBeVisible();
  expect(await fixtureTurns()).toHaveLength(0);
  await page.screenshot({ path: resolve(out, 'empty.png') });
  await frame.getByRole('button', { name: 'Load synthetic example', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Synthetic reading · a reproducible citation example', exact: true })).toBeVisible();
  expect((await library(frame)).papers).toHaveLength(1);
  expect(await fixtureTurns()).toHaveLength(0);
  expect(await frame.locator('body').evaluate(() => typeof window.pero)).toBe('undefined');
  checks.push('actual installed Python backend, explicit synthetic import, immutable reading, no App preload or implicit Agent run');
  await page.screenshot({ path: resolve(out, 'source.png') });
  await exerciseLocalSearch({ frame, page, out });
  expect(await fixtureTurns()).toHaveLength(0);
  checks.push('local indexed search, source digest, empty/error recovery, source opening and keyboard focus');

  await frame.getByRole('button', { name: 'Discover', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Confirm seed paper', exact: true })).toBeVisible();
  await frame.getByRole('button', { name: 'Use this match', exact: true }).click();
  const discovered = frame.locator('.pa-discovery [data-work-id="W2"]');
  await expect(discovered).toContainText('Synthetic W2');
  await discovered.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(discovered.getByRole('button', { name: 'Saved', exact: true })).toBeVisible();
  expect((await library(frame)).papers).toHaveLength(1);
  expect((await library(frame)).favorites).toHaveLength(1);
  await expect(frame.locator('.pa-research')).toHaveCount(0);
  await frame.getByRole('button', { name: 'Research', exact: true }).click();
  await expect(frame.locator('.pa-research-inputs')).toContainText('1/5');
  await frame.getByRole('button', { name: 'Research', exact: true }).click();
  await expect(frame.locator('.pa-research')).toHaveCount(0);
  const metadataOnly = frame.locator('.pa-discovery [data-work-id="W4"]');
  await metadataOnly.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(metadataOnly.getByRole('button', { name: 'Compare', exact: true })).toBeDisabled();
  await frame.getByRole('button', { name: 'Citing papers', exact: true }).click();
  await expect(frame.getByRole('alert')).toContainText('429');
  await frame.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(frame.getByText('No papers found', { exact: true })).toBeVisible();
  await frame.getByRole('button', { name: 'References', exact: true }).click();
  await expect(frame.locator('.pa-discovery-results>li')).toHaveCount(12);
  await frame.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(frame.locator('.pa-discovery-results>li')).toHaveCount(1);
  await frame.getByRole('button', { name: 'Related papers', exact: true }).click();
  await expect(frame.getByLabel('Exploration history', { exact: true })).not.toBeVisible();
  await expect(discovered.getByRole('button', { name: 'Read source', exact: true })).toHaveCount(0);
  await page.screenshot({ path: resolve(out, 'discovery-browse.png') });
  await discovered.getByRole('button', { name: 'Compare', exact: true }).click();
  await expect(frame.locator('.pa-research-inputs')).toContainText('2/5');
  await expect(frame.getByRole('status')).toContainText('Comparison updated');
  expect(await fixtureTurns()).toHaveLength(0);
  await page.screenshot({ path: resolve(out, 'discovery.png') });
  await frame.getByRole('button', { name: 'Back to seed paper', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Synthetic reading · a reproducible citation example', exact: true })).toBeVisible();
  checks.push('progressive disclosure: reading starts without research form, title opens reading, history/details stay folded, Compare opens setup; real offline OpenAlex adapter: explicit seed confirmation, related/reference/citing directions, pagination, error retry, metadata-only favorites and explicit matrix selection');

  await frame.locator('.pa-research-options>summary').click();
  await expect(frame.getByLabel('Agent', { exact: true })).toContainText('Synthetic Paper fixture');
  await frame.getByLabel('Agent', { exact: true }).selectOption('paper-fixture');
  await frame.getByLabel('Research question', { exact: true }).fill('What does this synthetic source establish?');
  await frame.getByRole('button', { name: 'Start research', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Synthetic research report', exact: true })).toBeVisible({ timeout: 30000 });
  expect(await savedCount(frame)).toBe(1);
  checks.push('valid synthetic ACP output is validated and saved automatically through the Python backend');
  await expect(frame.locator('.pa-scientific table')).toBeVisible();
  await expect(frame.locator('.pa-scientific .katex-display')).toBeVisible();
  await expect(frame.locator('.pa-scientific code .hljs-built_in')).toContainText('print');
  await expect(frame.getByRole('region', { name: 'Evidence matrix', exact: true })).toContainText('Synthetic fixture only');
  await expect(frame.locator('.pa-matrix td[data-status="missing"]').first()).toContainText('Not provided');
  await expect(frame.locator('.pa-matrix')).toContainText('Synthetic W2');
  await frame.getByRole('button', { name: 'Scope Citation evidence 1', exact: true }).click();
  await expect(frame.getByRole('complementary', { name: 'Citation evidence', exact: true }).locator('mark')).toBeVisible();
  await frame.getByRole('button', { name: 'Close', exact: true }).last().press('Escape');
  await expect(frame.getByRole('button', { name: 'Scope Citation evidence 1', exact: true })).toBeFocused();
  await frame.locator('.pa-scientific .pa-inline-citation').first().click();
  await expect(frame.getByRole('complementary', { name: 'Citation evidence', exact: true }).locator('mark')).toBeVisible();
  await frame.getByRole('complementary', { name: 'Citation evidence', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  await page.screenshot({ path: resolve(out, 'scientific-matrix.png') });
  checks.push('GFM table, KaTeX block/inline math, highlighted code, missing matrix cell, exact cell/inline citations, Escape and focus restoration');

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
  output.sections[0].content += '\n\n<script>window.paperInjected = true</script>\n\n[unsafe](javascript:alert(1))';
  output.sections[0].evidence[0].quote = paper.content.split('\n').find(line => line.trim() && !line.startsWith('#'));
  await raw.fill(JSON.stringify(output));
  await frame.getByRole('button', { name: 'Validate and save report', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'Corrected synthetic report', exact: true })).toBeVisible();
  expect(await savedCount(frame)).toBe(2);
  expect(await frame.locator('body').evaluate(() => window.paperInjected)).toBeUndefined();
  await expect(frame.locator('.pa-scientific script')).toHaveCount(0);
  await expect(frame.locator('.pa-scientific a[href^="javascript:"]')).toHaveCount(0);
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
  await frame.locator('.pa-matrix').scrollIntoViewIfNeeded();
  await frame.getByRole('button', { name: 'Scope 引用证据 1', exact: true }).click();
  await expect(frame.getByRole('complementary', { name: '引用证据', exact: true }).locator('mark')).toBeVisible();
  await page.screenshot({ path: resolve(out, 'narrow-matrix-evidence.png') });
  await frame.getByRole('complementary', { name: '引用证据', exact: true }).getByRole('button', { name: '关闭', exact: true }).press('Escape');
  await expect(frame.getByRole('button', { name: 'Scope 引用证据 1', exact: true })).toBeFocused();
  await page.screenshot({ path: resolve(out, 'narrow-matrix.png') });
  checks.push('live dark theme, Chinese locale and narrow window without horizontal document overflow');

  await frame.getByRole('tab', { name: /^收藏/ }).click();
  await frame.locator('[data-favorite-work="W2"] .pa-saved-actions>summary').click();
  await frame.getByRole('button', { name: '查看来源', exact: true }).first().click();
  await expect(frame.locator('.pa-discovery')).toContainText('Synthetic W2');
  await expect.poll(async () => frame.locator('html').evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  await frame.getByRole('button', { name: '后续引用', exact: true }).click();
  await expect(frame.locator('.pa-discovery [data-work-id="W2"]')).toHaveCount(0);
  await frame.getByRole('button', { name: '查看来源', exact: true }).first().click();
  await expect(frame.locator('.pa-discovery [data-work-id="W2"]')).toBeVisible();
  await page.screenshot({ path: resolve(out, 'discovery-dark-narrow.png') });
  await app.evaluate(({ Menu, BrowserWindow }) => { Menu.getApplicationMenu().getMenuItemById('locale-en').click(); BrowserWindow.getAllWindows()[0].setSize(1488, 1086); });
  await setAppearance(page, 'light');
  if (await frame.locator('.pa-research').count()) await frame.getByRole('button', { name: 'Research', exact: true }).click();
  const savedPdf = await exercisePdf({ frame, page, app, out, fixture: pdfFixture });
  checks.push('PDFium reading and manual matrix; full-text retrieval -> Host ACP answer -> exact PDF citation region');
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
  await expect.poll(async () => savedCount(frame)).toBe(3);
  await reopenPdf({ frame, saved: savedPdf });
  await frame.getByRole('tab', { name: /^Reports/ }).click();
  await frame.getByRole('button', { name: 'PDF answer fixture', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'PDF answer fixture', exact: true })).toBeVisible();
  await frame.locator('.pa-report .pa-scientific').getByRole('button', { name: 'Citation evidence 1', exact: true }).click();
  await expect(frame.locator('[data-citation-region="true"]').first()).toBeVisible();
  checks.push('PDF bytes, file digest, page anchors, annotations and matrix survive Desktop restart');
  expect((await library(frame)).favorites).toHaveLength(2);
  await frame.getByRole('tab', { name: /^Saved/ }).click();
  await frame.locator('[data-favorite-work="W2"] .pa-saved-actions>summary').click();
  await frame.locator('[data-favorite-work="W2"]').getByRole('button', { name: 'Compare', exact: true }).click();
  // Opening the PDF answer restores that paper before adding the two discovery sources.
  await expect(frame.locator('.pa-research-inputs')).toContainText('3/5');
  await expect(frame.locator('.pa-research-inputs li')).toHaveCount(3);
  await expect(frame.locator('.pa-research-inputs')).toContainText(savedPdf.paper.title);
  await expect(frame.getByRole('checkbox', { name: 'Search full PDF text' })).not.toBeChecked();
  await frame.getByRole('button', { name: 'View discovery source', exact: true }).first().click();
  await expect(frame.locator('.pa-discovery')).toContainText('Synthetic W2');
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
  }
  await expect.poll(() => Object.keys(payloads).length).toBe(2);
  for (const extension of ['js', 'css']) expect(payloads[extension]).toBe(createHash('sha256').update(await readFile(resolve(root, `apps/paper-agent/dist/index.${extension}`))).digest('hex'));
  expect(errors).toEqual([]);
  await writeFile(resolve(out, 'result.json'), JSON.stringify({ fixtureOnly: !pdfFixture.realPdf, realPdf: pdfFixture.realPdf, realModel: false, payloads, checks, errors, expectedConsole }, null, 2));
  console.log(JSON.stringify({ fixtureOnly: !pdfFixture.realPdf, realPdf: pdfFixture.realPdf, realModel: false, payloads, checks, errors, expectedConsole }));
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
