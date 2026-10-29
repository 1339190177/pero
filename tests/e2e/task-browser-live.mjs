// Opt-in, read-only acceptance of the deployed public discovery flow.
import assert from 'node:assert/strict';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { chromium, expect } from '@playwright/test';

if (process.env.PERO_DEPLOYMENT_E2E !== '1') throw new Error('Opt in with PERO_DEPLOYMENT_E2E=1');
const root = resolve(import.meta.dirname, '../..'), base = 'https://app.openpero.com';
const output = resolve(root, 'test-results/task-browser-live');
mkdirSync(output, { recursive: true });
const expectedRevision = process.env.PERO_TASK_BROWSER_REVISION;
const entries = [];
let domain = '', feature = '';
for (const [index, line] of readFileSync(resolve(root, 'ROADMAP.md'), 'utf8').split('\n').entries()) {
  if (line.startsWith('## ')) { domain = line.slice(3); feature = ''; }
  else if (line.startsWith('### ')) feature = line.slice(4);
  const match = line.match(/^\s*- \[([ xX])\] (.+)$/);
  if (match) entries.push({ domain, feature, line: index + 1, checked: match[1] !== ' ', title: match[2].split('. ')[0], goal: match[2] });
}
const notebookEntry = entries.find(entry => entry.title === '科研笔记本');
const notebookAcceptance = notebookEntry.goal.split('验收：')[1];
const checks = [], errors = [], started = new Date().toISOString();
let browser, page, report = { base, started, expected_revision: expectedRevision || null };
try {
  const [health, unauthorized, oauth] = await Promise.all([
    fetch(`${base}/healthz`, { signal: AbortSignal.timeout(15000) }),
    fetch(`${base}/api/v1/tasks`, { signal: AbortSignal.timeout(15000) }),
    fetch(`${base}/auth/github`, { redirect: 'manual', signal: AbortSignal.timeout(15000) }),
  ]);
  assert.equal(health.status, 200); assert.equal(unauthorized.status, 401);
  assert.ok([302, 303, 307].includes(oauth.status));
  assert.equal(new URL(oauth.headers.get('location')).hostname, 'github.com');
  checks.push('public-https-health', 'unauthenticated-api-denied', 'github-login-redirect-only');
  browser = await chromium.launch({ headless: true, executablePath: process.env.PERO_CHROMIUM_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1060 }, colorScheme: 'light' });
  page = await context.newPage();
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${base}/tasks`);
  await expect(page.locator('.task-browser')).toHaveClass(/enhanced/);
  const snapshot = await page.locator('.task-node').evaluateAll(nodes => nodes.map(node => {
    const href = node.querySelector(':scope > .node-info .node-actions a[href^="https://github.com/phodal/pero/blob/"]')?.href || '';
    const source = href.match(/\/blob\/([a-f0-9]{40})\/ROADMAP\.md#L(\d+)$/);
    const path = [];
    for (let category = node.closest('.category-node'); category; category = category.parentElement.closest('.category-node')) path.unshift(category.dataset.category);
    return { id: node.dataset.taskId, title: node.dataset.label, phase: node.dataset.phase, path,
      source: source ? { commit: source[1], line: Number(source[2]) } : null };
  }));
  const roadmap = snapshot.filter(task => task.source);
  const ledger = [...roadmap].sort((a, b) => a.source.line - b.source.line);
  assert.deepEqual(ledger.map(task => ({ title: task.title, line: task.source.line, path: task.path })),
    entries.map(entry => ({ title: entry.title, line: entry.line, path: [entry.domain, entry.feature] })));
  entries.forEach((entry, index) => assert.equal(['closed', 'accepted'].includes(ledger[index].phase), entry.checked || ledger[index].phase === 'accepted', entry.title));
  const commits = [...new Set(roadmap.map(task => task.source.commit))];
  assert.equal(commits.length, 1, 'current Roadmap should identify one pinned source revision');
  if (expectedRevision) assert.equal(commits[0], expectedRevision);
  const domains = [...new Set(entries.map(entry => entry.domain))];
  const rootLabels = await page.locator('.task-tree > .category-node').evaluateAll(nodes => nodes.map(node => node.dataset.category));
  const otherIndex = rootLabels.indexOf('其他任务');
  if (otherIndex >= 0) assert.ok(domains.every(label => rootLabels.indexOf(label) < otherIndex), 'functional domains should precede unclassified Issues');
  checks.push('functional-domains-before-other-tasks');
  const features = new Set(entries.map(entry => `${entry.domain}/${entry.feature}`));
  report = { ...report, source_commit: commits[0], public_tasks: snapshot.length, roadmap_tasks: roadmap.length,
    other_public_tasks: snapshot.length - roadmap.length, checked_sources: entries.filter(entry => entry.checked).length,
    domains: domains.length, features: features.size, titles: ledger.map(task => task.title) };
  checks.push('full-pinned-roadmap-title-line-heading-ledger', 'checked-source-phases', 'other-public-tasks-preserved');
  const search = page.getByRole('searchbox', { name: '搜索任务' });
  const status = page.getByRole('combobox', { name: '任务状态' });
  const unfinished = snapshot.filter(task => !['accepted', 'closed'].includes(task.phase)).length;
  report.unfinished_tasks = unfinished;
  await expect(status).toHaveValue('unfinished');
  await expect(page.locator('[data-results]')).toHaveText(`${unfinished} 项未完成任务`);
  await expect(page.locator('.task-node[data-phase="closed"]:not([hidden]),.task-node[data-phase="accepted"]:not([hidden])')).toHaveCount(0);
  for (const label of domains) {
    const category = page.locator('.task-tree > .category-node').filter({ has: page.locator(':scope > .task-group > .category-row .category-title').filter({ hasText: new RegExp(`^${label}$`) }) });
    await expect(category).toHaveCount(1);
  }
  await expect(page.locator('.category-node .category-node > .task-group[open]')).toHaveCount(0);
  await page.screenshot({ path: resolve(output, 'tree-light.png') });
  const firstFeature = page.locator('.category-node .category-node:not([hidden])').first();
  const featureSummary = firstFeature.locator(':scope > .task-group > .category-row');
  const domainSummary = firstFeature.locator('xpath=../..').locator(':scope > .category-row');
  await domainSummary.focus(); await page.keyboard.press('ArrowDown'); await expect(featureSummary).toBeFocused();
  await page.keyboard.press('ArrowRight'); await page.keyboard.press('ArrowDown');
  const firstTask = firstFeature.locator('.node-select:visible').first();
  await expect(firstTask).toBeFocused(); await page.keyboard.press('Enter');
  await expect(firstFeature.locator('.task-node.selected .node-info')).toHaveAttribute('open');
  await page.keyboard.press('Escape'); await expect(firstFeature.locator('.node-info[open]')).toHaveCount(0);
  await page.keyboard.press('ArrowLeft'); await expect(featureSummary).toBeFocused();
  checks.push('domain-feature-task-keyboard-and-escape');
  await status.selectOption('all');
  await expect(page.locator('[data-results]')).toHaveText(`${snapshot.length} 项任务`);
  await page.locator('.filter-reset').click();
  checks.push('completed-on-demand');

  await search.fill(notebookEntry.title);
  const notebookTask = roadmap.find(task => task.title === notebookEntry.title);
  const notebook = page.locator(`#task-${notebookTask.id}`), notebookButton = notebook.locator(':scope > .node-line .node-select');
  if (['closed', 'accepted'].includes(notebookTask.phase)) await status.selectOption('all');
  await expect(notebookButton).toBeVisible(); await notebookButton.click();
  await expect(notebook.locator('.description')).not.toContainText(`${notebookEntry.title}.`);
  await expect(notebook.locator('.description')).toContainText('逐节审阅');
  await expect(notebook.locator('.task-acceptance')).toContainText(notebookAcceptance);
  await expect(notebook.getByRole('link', { name: '在 Pero Desktop 中打开' })).toHaveAttribute('href', `pero://tasks/${notebookTask.id}`);
  await expect(notebook.getByRole('link', { name: '查看来源' })).toHaveAttribute('href', new RegExp(`/blob/${commits[0]}/ROADMAP\\.md#L${notebookEntry.line}$`));
  assert.equal(new URL(page.url()).pathname, '/tasks');
  await page.reload(); await expect(notebook.locator(':scope > .node-info')).toHaveAttribute('open');
  await expect(search).toHaveValue(notebookEntry.title);
  await page.screenshot({ path: resolve(output, 'inline-acceptance-light.png') });
  await search.fill(notebookAcceptance); await expect(notebookButton).toBeVisible();
  const path = notebook.locator('xpath=ancestor::li[contains(@class,"category-node")]');
  await expect(path.locator(':scope > .task-group:not([open])')).toHaveCount(0);
  checks.push('title-and-acceptance-search', 'ancestor-reveal', 'inline-scope-acceptance-and-desktop-source-links', 'selection-query-hash-reload');
  await page.locator('.filter-reset').click();
  await search.fill('Desktop');
  const matches = Number((await page.locator('[data-results]').innerText()).match(/^\d+/)[0]);
  assert.ok(matches > 0 && matches < unfinished, 'shared Desktop action must not match every task');
  await page.locator('.filter-reset').click();
  await status.selectOption('all');
  await page.getByRole('button', { name: '全景', exact: true }).click();
  for (const label of domains) await expect(page.locator('.panorama-heading').filter({ hasText: new RegExp(`^${label}`) })).toHaveCount(1);
  await page.screenshot({ path: resolve(output, 'panorama-light.png') });
  await page.locator('.panorama-card').filter({ hasText: notebookEntry.domain }).locator('.panorama-features button').filter({ hasText: notebookEntry.feature }).click();
  await expect(page.getByRole('button', { name: '树视图', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(notebookButton).toBeVisible();
  checks.push('action-labels-excluded-from-search', 'panorama-feature-drill-to-tree');
  await page.locator('.filter-reset').click(); await search.fill('电池模拟');
  await page.getByRole('button', { name: '全景', exact: true }).click();
  await expect(search).toHaveValue('电池模拟'); await page.reload();
  await expect(search).toHaveValue('电池模拟');
  await expect(page.getByRole('button', { name: '全景', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await page.locator('.filter-reset').click();
  await search.fill('不存在的任务片段live-acceptance');
  await expect(page.getByRole('heading', { name: '未完成任务中没有匹配项' })).toBeVisible();
  await page.locator('[data-no-results]').getByRole('button', { name: '清除筛选' }).click();
  await expect(search).toBeFocused(); await expect(status).toHaveValue('unfinished');
  checks.push('shared-view-filter-url-restoration', 'empty-recovery-to-unfinished');
  await page.setViewportSize({ width: 390, height: 844 });
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    for (const view of ['tree', 'panorama']) {
      await page.getByRole('button', { name: view === 'tree' ? '树视图' : '全景', exact: true }).click();
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${view}/${scheme} overflows`);
      await page.screenshot({ path: resolve(output, `${view}-mobile-${scheme}.png`) });
    }
  }
  const plain = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 1000, height: 800 } });
  const fallback = await plain.newPage(); await fallback.goto(`${base}/tasks`);
  await expect(fallback.locator('.node-fallback').first()).toBeVisible();
  await fallback.locator('.node-fallback').first().click();
  await expect(fallback.locator('.node-inspector').first()).toBeVisible();
  await expect(fallback.locator('.board-toolbar')).toBeHidden(); await plain.close();
  checks.push('390px-light-dark-tree-panorama', 'no-javascript-disclosure');
  assert.deepEqual(errors, []);
  report.result = 'passed';
  console.log(JSON.stringify({ result: report.result, source_commit: report.source_commit, roadmap: roadmap.length, other_tasks: report.other_public_tasks, checks: checks.length, errors }));
} catch (error) {
  report.result = 'failed'; report.failure = error.stack || String(error);
  if (page) await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
  throw error;
} finally {
  writeFileSync(resolve(output, 'verification.json'), JSON.stringify({ ...report, finished: new Date().toISOString(), checks, errors,
    boundary: 'Read-only public HTTPS/browser verification; no login, claim, execution, acceptance, points or GitHub writes.' }, null, 2));
  await browser?.close();
}
