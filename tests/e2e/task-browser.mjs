// Full Roadmap acceptance: finding unfinished work through domains and features.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { chromium, expect } from '@playwright/test';

const root = resolve(import.meta.dirname, '../..');
if (!process.env.PERO_TEST_DATABASE_URL) throw new Error('Set PERO_TEST_DATABASE_URL to a disposable PostgreSQL database.');
const output = resolve(root, 'test-results/task-browser');
mkdirSync(output, { recursive: true });
const scratch = mkdtempSync(resolve(tmpdir(), 'pero-task-browser-'));
const metadata = resolve(scratch, 'fixture.json');
const child = spawn('go', ['test', '-v', '-count=1', '-run', '^TestCommunityBrowserFixture$', './internal/community'], {
  cwd: resolve(root, 'server'), env: { ...process.env, PERO_COMMUNITY_BROWSER_FIXTURE: metadata, PERO_TASK_BROWSER_SOURCE: resolve(root, 'ROADMAP.md') }, stdio: ['ignore', 'pipe', 'pipe'],
});
let logs = '', browser;
child.stdout.on('data', chunk => { logs += chunk; }); child.stderr.on('data', chunk => { logs += chunk; });
const finished = new Promise(resolve => child.on('exit', resolve));
const source = readFileSync(resolve(root, 'ROADMAP.md'), 'utf8');
const entries = [];
let domain = '', feature = '';
for (const line of source.split('\n')) {
  if (line.startsWith('## ')) { domain = line.slice(3); feature = ''; }
  else if (line.startsWith('### ')) feature = line.slice(4);
  const match = line.match(/^\s*- \[([ xX])\] (.+)$/);
  if (match) entries.push({ domain, feature, closed: match[1] !== ' ', title: match[2].split('. ')[0], goal: match[2] });
}
const total = entries.length, closed = entries.filter(entry => entry.closed).length;
const unfinished = total - closed;
const domainCount = new Set(entries.filter(entry => !entry.closed).map(entry => entry.domain)).size;
const notebookEntry = entries.find(entry => entry.title === '科研笔记本');
assert.ok(notebookEntry?.goal.includes('验收：'), 'notebook should have an explicit acceptance criterion');
const notebookAcceptance = notebookEntry.goal.split('验收：')[1];
try {
  const deadline = Date.now() + 30_000;
  while (!existsSync(metadata)) {
    if (child.exitCode !== null || Date.now() > deadline) throw new Error(`Fixture failed: ${logs}`);
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  const { url } = JSON.parse(readFileSync(metadata, 'utf8'));
  browser = await chromium.launch({ headless: true, ...(process.env.PERO_CHROMIUM_PATH ? { executablePath: process.env.PERO_CHROMIUM_PATH } : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1060 }, colorScheme: 'light' });
  const page = await context.newPage(), errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${url}/tasks`);
  await expect(page.locator('.task-browser')).toHaveClass(/enhanced/);
  await expect(page.locator('.task-node')).toHaveCount(total);
  assert.ok(total > 50, 'full dataset must cross old pagination');
  const search = page.getByRole('searchbox', { name: '搜索任务' });
  const status = page.getByRole('combobox', { name: '任务状态' });
  const results = page.locator('[data-results]');
  await expect(status).toHaveValue('unfinished');
  await expect(results).toHaveText(`${unfinished} 项未完成任务`);
  await expect(page.locator('.task-tree > .category-node:not([hidden])')).toHaveCount(domainCount);
  await expect(page.locator('.category-node .category-node > .task-group[open]')).toHaveCount(0);
  await expect(page.locator('.task-node[data-phase="closed"]:not([hidden])')).toHaveCount(0);
  await expect(page.locator('.node-select:visible')).toHaveCount(0);
  await expect(page.locator('[data-category-filter=""] .nav-count')).toHaveText(String(unfinished));
  await page.screenshot({ path: resolve(output, 'tree-light.png'), fullPage: false });

  const firstDomain = page.locator('.task-tree > .category-node:not([hidden])').first();
  const firstCategory = firstDomain.locator(':scope > .task-group > .category-row');
  const firstFeature = firstDomain.locator('.category-node:not([hidden])').first();
  const featureSummary = firstFeature.locator(':scope > .task-group > .category-row');
  await firstCategory.focus();
  await page.keyboard.press('ArrowDown');
  await expect(featureSummary).toBeFocused();
  await page.keyboard.press('ArrowRight');
  const firstLeaf = firstFeature.locator('.node-select:visible').first();
  await expect(firstLeaf).toBeVisible();
  await page.keyboard.press('ArrowDown');
  await expect(firstLeaf).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(firstFeature.locator('.task-node.selected .node-info')).toHaveAttribute('open');
  await page.keyboard.press('Escape');
  await expect(firstFeature.locator('.node-info[open]')).toHaveCount(0);
  await page.keyboard.press('ArrowLeft');
  await expect(featureSummary).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await expect(firstFeature.locator(':scope > .task-group')).not.toHaveAttribute('open');
  await page.keyboard.press('ArrowLeft');
  await expect(firstCategory).toBeFocused();
  await page.getByRole('button', { name: '收起全部', exact: true }).click();
  await expect(page.locator('.node-select:visible')).toHaveCount(0);
  await firstCategory.focus();
  await page.keyboard.press('ArrowRight');
  await expect(featureSummary).toBeVisible();
  await page.getByRole('button', { name: '展开全部', exact: true }).click();
  await expect(page.locator('.node-select:visible')).toHaveCount(unfinished);
  // A collapsed selection must not remove the visible keyboard entry point.
  await firstLeaf.click();
  await featureSummary.click();
  await expect(firstLeaf).toBeHidden();
  await page.keyboard.press('Escape');
  await expect(featureSummary).toBeFocused();
  await expect(page.locator('.category-row[tabindex="0"]:visible')).toHaveCount(1);
  await featureSummary.click();

  // Common action labels must not make every task match a meaningful query.
  const desktopCount = entries.filter(entry => !entry.closed && `${entry.domain} ${entry.feature} ${entry.goal}`.toLowerCase().includes('desktop')).length;
  assert.ok(desktopCount > 0 && desktopCount < unfinished);
  await search.fill('Desktop');
  await expect(results).toHaveText(`${desktopCount} 项未完成任务`);
  await page.locator('.filter-reset').click();


  await status.selectOption('closed');
  await expect(results).toHaveText(`${closed} 项任务`);
  await expect(page.locator('.task-node[data-phase="closed"]:visible')).toHaveCount(closed);
  await expect(page.locator('[data-category-filter=""] .nav-count')).toHaveText(String(closed));
  await status.selectOption('all');
  await expect(results).toHaveText(`${total} 项任务`);
  await expect(page.locator('.node-select:visible')).toHaveCount(total);
  await status.selectOption('closed');
  await page.getByRole('button', { name: '全景', exact: true }).click();
  await expect(page.locator('.panorama-features button')).not.toHaveCount(0);
  await expect(status).toHaveValue('closed');
  await page.locator('.filter-reset').click();
  await expect(status).toHaveValue('unfinished');
  const cardCount = await page.locator('.panorama-card').count();
  assert.equal(cardCount, domainCount, 'panorama should show functional domains');
  await page.screenshot({ path: resolve(output, 'panorama-light.png') });
  await page.screenshot({ path: resolve(output, 'panorama-full.png'), fullPage: true });
  await page.locator('.panorama-card').filter({ hasText: notebookEntry.domain }).locator('.panorama-features button').filter({ hasText: notebookEntry.feature }).click();
  await expect(page.getByRole('button', { name: '树视图', exact: true })).toHaveAttribute('aria-pressed', 'true');
  const notebook = page.locator('.node-select').filter({ hasText: /^科研笔记本$/ });
  await expect(notebook).toBeVisible();
  await notebook.click();
  await expect(page.locator('.task-node.selected .node-inspector')).toContainText('逐节审阅');
  await expect(page.locator('.task-node.selected .description')).not.toContainText('科研笔记本.');
  await expect(page.locator('.task-node.selected .task-acceptance')).toBeVisible();
  await expect(page.locator('.task-node.selected .task-acceptance')).toContainText(notebookAcceptance);
  assert.equal(new URL(page.url()).pathname, '/tasks');
  await expect(page.locator('.task-node.selected .node-actions a').first()).toHaveAttribute('href', /^pero:\/\/tasks\//);
  await page.screenshot({ path: resolve(output, 'selected-inline-light.png') });
  await page.getByRole('button', { name: '全景', exact: true }).click();
  await page.getByRole('button', { name: '树视图', exact: true }).click();
  await expect(page.locator('.task-node.selected .node-select')).toHaveText('科研笔记本');
  await page.locator('.filter-reset').click();

  await search.fill(notebookAcceptance);
  await expect(results).toHaveText('1 项未完成任务');
  await expect(notebook).toBeVisible();
  await expect(page.locator('.category-node:visible > .task-group:not([open])')).toHaveCount(0);
  await expect(page.locator('[data-category-filter=""] .nav-count')).toHaveText('1');
  const matchingDomain = page.locator('.category-button').filter({ has: page.locator('span').filter({ hasText: new RegExp(`^${notebookEntry.domain}$`) }) });
  await expect(matchingDomain.locator('.nav-count')).toHaveText('1');
  await expect(page.locator('.category-button:not(:disabled)')).toHaveCount(2);
  await page.locator('.filter-reset').click();
  await search.fill('不存在的任务片段abcd');
  await expect(page.getByRole('heading', { name: '未完成任务中没有匹配项' })).toBeVisible();
  await page.locator('[data-no-results]').getByRole('button', { name: '清除筛选' }).click();
  await expect(search).toBeFocused();
  await expect(status).toHaveValue('unfinished');
  await expect(results).toHaveText(`${unfinished} 项未完成任务`);
  await search.fill('电池模拟');
  await expect(results).toHaveText('1 项未完成任务');
  await expect(page.locator('.node-select').filter({ hasText: /^电池模拟$/ })).toBeVisible();
  await page.getByRole('button', { name: '全景', exact: true }).click();
  await page.reload();
  await expect(search).toHaveValue('电池模拟');
  await expect(results).toHaveText('1 项未完成任务');
  await expect(page.getByRole('button', { name: '全景', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await page.getByRole('button', { name: '树视图', exact: true }).click();
  await page.locator('.filter-reset').click();

  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: 'dark' });
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'tree page overflows narrow viewport');
  await page.screenshot({ path: resolve(output, 'tree-mobile-dark.png') });
  await page.getByRole('button', { name: '全景', exact: true }).click();
  await expect(page.locator('.panorama-card')).toHaveCount(cardCount);
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'panorama overflows narrow viewport');
  await page.screenshot({ path: resolve(output, 'panorama-mobile-dark.png') });
  assert.deepEqual(errors, []);
  assert.ok(!(await page.locator('body').innerText()).includes('PRIVATE_PROMPT_SENTINEL'));

  const plain = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 1000, height: 800 } });
  const fallback = await plain.newPage(); await fallback.goto(`${url}/tasks`);
  await expect(fallback.locator('.node-fallback').first()).toBeVisible();
  await fallback.locator('.node-fallback').first().click();
  await expect(fallback.locator('.node-inspector').first()).toBeVisible();
  await expect(fallback.locator('.board-toolbar')).toBeHidden();
  await plain.close();
  writeFileSync(resolve(output, 'verification.json'), JSON.stringify({ result: 'passed', tasks: total, unfinished, closed_sources: closed, domains: cardCount, cases: ['full-public-outline-over-50', 'unfinished-default', 'collapsed-feature-map', 'domain-feature-task-keyboard', 'completed-on-demand', 'filtered-domain-counts', 'shared-view-filters', 'panorama-feature-to-tree', 'inline-scope-and-acceptance', 'search-acceptance-with-ancestors', 'action-labels-excluded-from-search', 'collapsed-selection-focus-recovery', 'inline-selection-preserved', 'empty-recovery', 'url-view-query-restoration', '390px-light-dark', 'no-js-disclosure', 'no-private-goal'], errors }, null, 2));
  console.log(JSON.stringify({ tasks: total, categories: cardCount, errors }));
} finally {
  await browser?.close();
  writeFileSync(`${metadata}.stop`, 'stop');
  const code = await Promise.race([finished, new Promise(resolve => setTimeout(() => resolve('timeout'), 10000))]);
  if (code === 'timeout') child.kill('SIGTERM'); else if (code !== 0) console.error(logs);
  rmSync(scratch, { recursive: true, force: true });
}
