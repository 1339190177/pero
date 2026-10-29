// Shared gestures for the fixed sidebar footer; all choices use the visible menu.
import { expect } from '@playwright/test';
const state = page => page.evaluate(() => JSON.parse(window.peroDiagnostics.snapshot()));
export async function openSettings(page) {
  await page.mouse.click(100, await page.evaluate(() => innerHeight - 26));
}
export async function setAppearance(page, appearance) {
  await openSettings(page);
  // Start at the last action so account/header labels do not affect navigation.
  for (let i = 0; i < 3; i++) await page.keyboard.press('ArrowUp');
  await page.keyboard.press('ArrowRight');
  for (let i = 0; i < ['light', 'dark', 'system'].indexOf(appearance); i++) await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await state(page)).preferences.appearance).toBe(appearance);
}
export async function setSidebarLocale(page, locale) {
  await openSettings(page);
  for (let i = 0; i < 2; i++) await page.keyboard.press('ArrowUp');
  await page.keyboard.press('ArrowRight');
  if (locale === 'en') await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await state(page)).locale).toBe(locale);
}
