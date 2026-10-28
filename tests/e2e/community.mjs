import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium, expect } from '@playwright/test';

const root = fileURLToPath(new URL('../..', import.meta.url));
if (!process.env.PERO_TEST_DATABASE_URL) throw new Error('Set PERO_TEST_DATABASE_URL to an isolated PostgreSQL database.');
const scratch = mkdtempSync(resolve(tmpdir(), 'pero-community-browser-'));
const metadata = resolve(scratch, 'fixture.json');
const output = resolve(root, 'test-results/community');
mkdirSync(output, { recursive: true });
const child = spawn('go', ['test', '-v', '-count=1', '-run', '^TestCommunityBrowserFixture$', './internal/community'], {
  cwd: resolve(root, 'server'),
  env: { ...process.env, PERO_COMMUNITY_BROWSER_FIXTURE: metadata },
  stdio: ['ignore', 'pipe', 'pipe'],
});
let logs = '';
child.stdout.on('data', chunk => { logs += chunk; });
child.stderr.on('data', chunk => { logs += chunk; });
const finished = new Promise(resolve => child.on('exit', code => resolve(code)));
let browser;
let succeeded = false;
try {
  const deadline = Date.now() + 30_000;
  while (!existsSync(metadata)) {
    if (child.exitCode !== null || Date.now() > deadline) throw new Error(`Fixture failed: ${logs}`);
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  const { url, task, worker, workerToken, changed_task, closed_task } = JSON.parse(readFileSync(metadata, 'utf8'));
  browser = await chromium.launch({ headless: true, ...(process.env.PERO_CHROMIUM_PATH ? { executablePath: process.env.PERO_CHROMIUM_PATH } : {}) });
  const admin = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light' });
  const member = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light' });
  const ap = await admin.newPage();
  const mp = await member.newPage();
  const errors = [];
  for (const p of [ap, mp]) p.on('pageerror', err => { errors.push(err.message); console.error('Browser error:', err.message); });
  await mp.goto(`${url}/tasks`);
  await expect(mp.getByRole('heading', { name: '还没有公开任务' })).toBeVisible();
  await mp.keyboard.press('Tab');
  await expect(mp.getByRole('link', { name: '跳到主要内容' })).toBeFocused();
  await mp.keyboard.press('Enter');
  await expect(mp.getByRole('main')).toBeFocused();
  await mp.screenshot({ path: resolve(output, 'empty-light.png') });
  await ap.goto(`${url}/community/admin/login`);
  await ap.getByLabel('管理员凭证').fill('incorrect');
  await ap.getByRole('button', { name: '登录管理台' }).click();
  await expect(ap.getByRole('alert')).toContainText('凭证无效');
  await ap.getByLabel('管理员凭证').fill('fixture-operator');
  await ap.getByRole('button', { name: '登录管理台' }).click();
  await expect(ap.getByRole('heading', { name: '社区管理', exact: true })).toBeVisible();
  await ap.waitForLoadState('load');
  await ap.getByLabel('已有任务').fill(task);
  await ap.getByLabel('公开标题').fill('为远程任务添加可恢复的进度视图');
  await ap.getByLabel('公开任务说明与验收要求').fill('恢复连接后继续展示任务进度，保留已有执行记录。\n验收要求：断线后重新连接，不重复执行任务。');
  await ap.getByLabel('来源链接（可选）').fill('https://github.com/phodal/pero/issues/2');
  // Observe feedback during submit; locator actions wait for pending native navigation.
  await ap.evaluate(() => document.querySelector('form[action="/community/admin/publish"]').addEventListener('submit', event => {
    const busy = document.getElementById('busy');
    console.log(`community-submission:${JSON.stringify({ visible: !busy.hidden && getComputedStyle(busy).display !== 'none', disabled: event.target.querySelector('button').disabled })}`);
  }));
  let releasePublish;
  const publishGate = new Promise(resolve => { releasePublish = resolve; });
  await ap.route('**/community/admin/publish', async route => { await publishGate; await route.continue(); });
  const submission = ap.waitForEvent('console', { predicate: msg => msg.text().startsWith('community-submission:') });
  const publishing = ap.getByRole('button', { name: '发布到任务广场' }).click();
  assert.deepEqual(JSON.parse((await submission).text().slice('community-submission:'.length)), { visible: true, disabled: true });
  releasePublish();
  await publishing;
  await ap.unroute('**/community/admin/publish');
  await expect(ap.getByRole('heading', { name: '为远程任务添加可恢复的进度视图' })).toBeVisible();
  await mp.goto(`${url}/auth/github`); // local OAuth fixture, no real GitHub login
  await expect(mp.getByRole('link', { name: 'alice', exact: true })).toBeVisible();
  await mp.goto(`${url}/tasks/${task}`);
  await expect(mp.getByRole('button', { name: '认领并执行' })).toBeDisabled();
  await expect(mp.getByText('你还没有该项目的 Worker。')).toBeVisible();
  await ap.goto(`${url}/community/admin`);
  await ap.getByLabel('Worker ID', { exact: true }).fill(worker);
  await ap.getByLabel('GitHub 用户名').fill('alice');
  await ap.getByRole('button', { name: '绑定 Worker' }).click();
  await expect(ap.getByRole('status')).toContainText('Worker 已绑定');
  await mp.reload();
  await expect(mp.getByLabel('执行 Worker')).toHaveValue(worker);
  await mp.getByRole('button', { name: '认领并执行' }).click();
  await expect(mp.getByText('排队中', { exact: true })).toBeVisible();
  const workerRequest = async (path, body) => {
    const response = await fetch(`${url}${path}`, { method: 'POST', headers: { Authorization: `Bearer ${workerToken}`, 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    assert.equal(response.status, 200, await response.clone().text());
    return response.json();
  };
  const { run } = await workerRequest('/api/worker/v1/claims', { instance_id: 'browser-fixture', request_id: 'first' });
  assert.ok(run?.id);
  await mp.reload();
  await expect(mp.getByText('执行中', { exact: true })).toBeVisible();
  await mp.screenshot({ path: resolve(output, 'running-light.png') });
  await workerRequest(`/api/worker/v1/runs/${run.id}/complete`, { instance_id: 'browser-fixture', generation: run.generation, outcome: 'succeeded', summary: 'Fixture execution outcome; independent review still required.' });
  await mp.goto(`${url}/leaderboard?period=all`);
  await expect(mp.getByRole('heading', { name: '还没有验收贡献' })).toBeVisible();
  await ap.goto(`${url}/tasks/${task}`);
  await ap.getByLabel('PR 或验收报告链接').fill('https://github.com/phodal/pero/pull/2');
  await ap.getByLabel('公开验收说明').fill('已核对断线恢复行为与执行记录，满足公开任务的验收要求。');
  await ap.getByRole('button', { name: '验收通过，计 1 分' }).click();
  await expect(ap.getByText('已验收', { exact: true })).toBeVisible();
  await mp.goto(`${url}/leaderboard`);
  await expect(mp.getByRole('cell', { name: 'alice', exact: true })).toBeVisible();
  await mp.screenshot({ path: resolve(output, 'leaderboard-light.png') });
  await mp.getByRole('link', { name: '全部', exact: true }).click();
  await expect(mp.getByRole('link', { name: '全部', exact: true })).toHaveAttribute('aria-current', 'page');
  await mp.emulateMedia({ colorScheme: 'dark' });
  await mp.screenshot({ path: resolve(output, 'leaderboard-dark.png') });
  await mp.goto(`${url}/tasks`);
  await mp.screenshot({ path: resolve(output, 'tasks-dark.png') });
  await mp.setViewportSize({ width: 390, height: 844 });
  await mp.screenshot({ path: resolve(output, 'tasks-mobile-dark.png') });
  assert.ok(await mp.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'page overflow on mobile');
  await mp.goto(`${url}/contributors/alice`);
  await expect(mp.getByText('+1', { exact: true })).toBeVisible();
  await expect(mp.getByRole('link', { name: '查看验收证据 ↗' })).toHaveAttribute('href', 'https://github.com/phodal/pero/pull/2');
  await mp.screenshot({ path: resolve(output, 'contributor-mobile-dark.png') });
  const publicText = await mp.locator('body').innerText();
  for (const secret of ['PRIVATE_PROMPT_SENTINEL', 'PRIVATE_WORKSPACE_SENTINEL', 'PRIVATE_ACCEPTANCE_SENTINEL']) assert.ok(!publicText.includes(secret));
  await ap.getByText('撤销此笔验收积分', { exact: true }).click();
  await ap.getByLabel('公开撤销原因').fill('复核发现结果仍有遗漏，撤销积分并要求重新执行。');
  await ap.getByLabel('确认撤销 alice 的这 1 分').check();
  await ap.getByRole('button', { name: '确认撤销积分' }).click();
  await expect(ap.getByText('验收已撤销', { exact: true })).toBeVisible();
  await mp.goto(`${url}/leaderboard?period=all`);
  await expect(mp.getByRole('heading', { name: '还没有验收贡献' })).toBeVisible();
  await mp.goto(`${url}/contributors/alice`);
  await expect(mp.getByText('−1', { exact: true })).toBeVisible();
  for (const [sourceTask, title, label] of [
    [changed_task, '来源更新后需要核对的任务', '来源已更新'],
    [closed_task, '来源关闭后暂停认领的任务', '来源已关闭'],
  ]) {
    await ap.goto(`${url}/community/admin`);
    await ap.getByLabel('已有任务').fill(sourceTask);
    await ap.getByLabel('公开标题').fill(title);
    await ap.getByLabel('公开任务说明与验收要求').fill('这是经过检查的公开要求。');
    await ap.getByRole('button', { name: '发布到任务广场' }).click();
    await mp.goto(`${url}/tasks/${sourceTask}`);
    await expect(mp.locator('.badge')).toHaveText(label);
    await expect(mp.getByRole('button', { name: '认领并执行' })).toHaveCount(0);
    assert.ok(!(await mp.locator('body').innerText()).includes('PRIVATE_REVISED_SOURCE_GOAL'));
  }
  await mp.goto(`${url}/tasks/missing`);
  await expect(mp.getByRole('alert')).toContainText('未找到');
  await mp.screenshot({ path: resolve(output, 'error-mobile-dark.png') });
  await mp.getByRole('link', { name: '返回任务广场' }).click();
  await expect(mp.getByRole('heading', { name: '任务广场', exact: true })).toBeVisible();
  const unavailable = await mp.goto(`${url}/__fixture/unavailable`);
  assert.equal(unavailable.status(), 503);
  await expect(mp.getByRole('alert')).toContainText('服务暂时不可用');
  await mp.getByRole('link', { name: '返回任务广场' }).click();
  assert.deepEqual(errors, []);
  writeFileSync(resolve(output, 'verification.json'), JSON.stringify({ result: 'passed', cases: ['empty', 'keyboard-focus', 'loading-feedback', 'login-error', 'publish', 'oauth-fixture', 'worker-disabled', 'worker-binding', 'claim', 'running', 'success-without-points', 'accept', 'weekly/all', 'revoke', 'history', '404-recovery', '503-recovery', 'light/dark', '390px-no-page-overflow', 'changed-source-blocked', 'closed-source-blocked'], provider: 'local OAuth and Worker protocol fixtures; no real GitHub or Agent execution' }, null, 2));
  console.log(`Community browser checks passed. Screenshots: ${output}`);
  succeeded = true;
} finally {
  await browser?.close();
  writeFileSync(`${metadata}.stop`, 'stop');
  const code = await Promise.race([finished, new Promise(resolve => setTimeout(() => resolve('timeout'), 10_000))]);
  if (code === 'timeout') child.kill('SIGTERM');
  else if (code !== 0) console.error(logs);
  if (!succeeded) console.error(logs);
  rmSync(scratch, { recursive: true, force: true });
}
