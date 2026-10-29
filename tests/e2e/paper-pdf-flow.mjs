import { expect } from '@playwright/test';
import { resolve } from 'node:path';
import { setAppearance } from './sidebar-controls.mjs';

export async function exercisePdf({ frame, page, app, out, fixture }) {
  const post = (path, body) => frame.locator('body').evaluate(async (_element, { path, body }) => {
    const response = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const value = await response.json(); if (!response.ok) throw new Error(JSON.stringify(value)); return value;
  }, { path, body });
  const paper = await post('/api/papers/import', { title: fixture.realPdf ? 'Attention Is All You Need · PDF' : 'Synthetic PDF reading', sourceId: '1706.03762',
    url: 'https://arxiv.org/abs/1706.03762', pdfUrl: 'https://arxiv.org/pdf/1706.03762', authors: [], contentKind: 'full-text', content: fixture.text });
  await frame.getByRole('tab', { name: /^Papers/ }).click();
  await frame.locator('.pa-library-footer').getByRole('button', { name: 'Refresh', exact: true }).click();
  await frame.getByRole('button', { name: paper.title, exact: true }).click();
  await frame.getByRole('button', { name: 'PDF', exact: true }).click();
  const viewer = frame.getByRole('region', { name: 'PDF reader', exact: true });
  await expect(viewer).toBeVisible({ timeout: 30000 });
  const first = viewer.locator('[data-page-number="1"]');
  await expect(first.locator('img,canvas').first()).toBeVisible({ timeout: 30000 });
  await expect(first).toHaveAttribute('data-selection-ready', 'true', { timeout: 30000 });
  const reading = () => frame.locator('body').evaluate(async (_element, id) => (await fetch(`/api/pdf?paperId=${id}`)).json(), paper.id);
  expect((await reading()).document.pages).toHaveLength(fixture.pages);
  // Select actual PDF glyphs with pointer events, rather than injecting an annotation.
  const box = await first.boundingBox(), s = fixture.selection;
  await page.mouse.move(box.x + s.x1 * box.width, box.y + s.y * box.height);
  await page.mouse.down();
  await page.mouse.move(box.x + s.x2 * box.width, box.y + s.y * box.height, { steps: 20 });
  await page.mouse.up();
  await expect(viewer.getByRole('button', { name: 'Highlight', exact: true })).toBeVisible();
  await viewer.getByRole('textbox', { name: 'Note (optional)' }).fill('Saved source evidence');
  await viewer.getByRole('button', { name: 'Highlight', exact: true }).click();
  await expect.poll(async () => (await reading()).annotations.length).toBe(1);
  expect((await reading()).annotations[0].quote).toContain(fixture.quote);
  await expect(first.locator('[data-kind="highlight"]').first()).toBeVisible();
  await page.screenshot({ path: resolve(out, 'pdf-highlight.png') });

  await viewer.getByRole('spinbutton', { name: 'Page', exact: true }).fill(fixture.realPdf ? '3' : '2');
  const targetPage = fixture.realPdf ? 3 : 2;
  const target = viewer.locator(`[data-page-number="${targetPage}"]`);
  await expect(target.locator('img,canvas').first()).toBeVisible();
  await expect(viewer.getByRole('spinbutton', { name: 'Page', exact: true })).toHaveValue(String(targetPage));
  await viewer.getByRole('button', { name: 'Capture area', exact: true }).click();
  const region = await target.boundingBox();
  await page.mouse.move(region.x + region.width * .15, region.y + region.height * .15);
  await page.mouse.down();
  await page.mouse.move(region.x + region.width * .7, region.y + region.height * .45, { steps: 20 });
  await page.mouse.up();
  await expect.poll(async () => (await reading()).annotations.length).toBe(2);
  await viewer.getByRole('button', { name: 'Annotations 2', exact: true }).click();
  const result = await reading(), figure = result.annotations.find(item => item.kind === 'figure');
  const image = viewer.locator(`[data-note-id="${figure.id}"] img`);
  await expect(image).toBeVisible();
  expect(await image.evaluate(element => element.naturalWidth)).toBeGreaterThan(100);
  await page.screenshot({ path: resolve(out, 'pdf-figure.png') });
  await app.evaluate(async ({ clipboard, ClipboardItem }) => { globalThis.paperClipboard = await Promise.all((await clipboard.read()).map(async item => new ClipboardItem(Object.fromEntries(await Promise.all(item.types.map(async type => [type, await item.getType(type)])))))); });
  try {
    await viewer.locator(`[data-note-id="${figure.id}"]`).getByRole('button', { name: 'Copy image', exact: true }).click();
    await expect(viewer.getByRole('status')).toContainText('Image copied');
    expect(await app.evaluate(({ clipboard }) => clipboard.has('image/png'))).toBe(true);
  } finally {
    await app.evaluate(async ({ clipboard }) => { if (globalThis.paperClipboard.length) await clipboard.write(globalThis.paperClipboard); else clipboard.clear(); delete globalThis.paperClipboard; });
  }

  const note = viewer.locator(`[data-note-id="${figure.id}"]`);
  await note.getByRole('button', { name: 'Add to matrix', exact: true }).click();
  await note.getByRole('textbox', { name: 'Comparison field' }).fill('Method');
  await note.locator('.pa-pdf-matrix-field').getByRole('button', { name: 'Add to matrix', exact: true }).click();
  const cell = frame.locator(`[data-matrix-annotation="${figure.id}"]`);
  await expect(cell).toBeVisible();
  await expect(cell.locator('img')).toBeVisible();
  await page.screenshot({ path: resolve(out, 'pdf-matrix.png') });
  await cell.getByRole('button', { name: `Page ${targetPage} ↗`, exact: true }).click();
  await expect(viewer.getByRole('spinbutton', { name: 'Page', exact: true })).toHaveValue(String(targetPage));
  await expect(viewer.locator(`[data-annotation-id="${figure.id}"][data-focused="true"]`)).toBeVisible();
  await viewer.getByRole('button', { name: 'Zoom in', exact: true }).click();
  await viewer.getByRole('button', { name: 'Fit width', exact: true }).click();
  await setAppearance(page, 'dark');
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1000, 800));
  await expect.poll(async () => frame.locator('html').evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  await expect.poll(async () => {
    const pageBox = await viewer.locator(`[data-page-number="${targetPage}"]`).boundingBox();
    const viewportBox = await viewer.locator('.pa-pdf-viewport').boundingBox();
    return !!pageBox && !!viewportBox && pageBox.width <= viewportBox.width + 1;
  }).toBe(true);
  await expect(viewer.locator(`[data-annotation-id="${figure.id}"]`)).toBeVisible();
  await page.screenshot({ path: resolve(out, 'pdf-dark-narrow.png') });
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1488, 1086));
  await setAppearance(page, 'light');
  const unavailable = await post('/api/papers/import', { title: 'Synthetic unavailable PDF', sourceId: 'unavailable-pdf',
    url: 'https://example.invalid/source', pdfUrl: 'https://example.invalid/paper.pdf', authors: [], contentKind: 'abstract', content: 'Synthetic source remains readable.' });
  await frame.locator('.pa-library-footer').getByRole('button', { name: 'Refresh', exact: true }).click();
  await frame.getByRole('button', { name: unavailable.title, exact: true }).click();
  await frame.getByRole('button', { name: 'PDF', exact: true }).click();
  await expect(frame.locator('.pa-pdf-reading [role="alert"]')).toContainText('PDF URL');
  await expect(frame.locator('.pa-pdf-reading').getByRole('button', { name: 'Retry', exact: true })).toBeEnabled();
  await frame.getByRole('button', { name: 'Text', exact: true }).click();
  await expect(frame.locator('.pa-source-text')).toContainText('Synthetic source remains readable.');
  await expect(frame.locator('.pa-pdf-reading [role="alert"]')).toHaveCount(0);
  await frame.getByRole('button', { name: paper.title, exact: true }).click();
  await expect(viewer).toBeVisible();
  await expect(viewer.locator('[data-page-number="1"] img')).toBeVisible();
  return { paper, figure, fileDigest: result.document.fileDigest, targetPage };
}

export async function reopenPdf({ frame, saved }) {
  await frame.getByRole('tab', { name: 'Evidence matrix', exact: true }).click();
  const cell = frame.locator(`[data-matrix-annotation="${saved.figure.id}"]`);
  await expect(cell).toBeVisible();
  await cell.getByRole('button', { name: `Page ${saved.targetPage} ↗`, exact: true }).click();
  const viewer = frame.getByRole('region', { name: 'PDF reader', exact: true });
  await expect(viewer.getByRole('spinbutton', { name: 'Page', exact: true })).toHaveValue(String(saved.targetPage));
  await expect(viewer.locator(`[data-annotation-id="${saved.figure.id}"]`)).toBeVisible();
  const reading = await frame.locator('body').evaluate(async (_element, id) => (await fetch(`/api/pdf?paperId=${id}`)).json(), saved.paper.id);
  expect(reading.document.fileDigest).toBe(saved.fileDigest);
  expect(reading.annotations).toHaveLength(2);
}
