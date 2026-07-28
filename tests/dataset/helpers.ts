// Shared helpers for the synthetic data-entry run.
//
// Everything here drives the real UI: navigate, fill the form, submit, assert we
// landed somewhere sane. Nothing writes to SQLite directly — the point of the
// exercise is that the application itself produced the data.

import { expect, type Page } from '@playwright/test';
import { mkdirSync, readFileSync, readdirSync, writeFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

export const ARTIFACT_DIR = 'output/dataset';

export function saveArtifact(name: string, value: unknown): void {
  mkdirSync(ARTIFACT_DIR, { recursive: true });
  writeFileSync(join(ARTIFACT_DIR, name), JSON.stringify(value, null, 2));
}

export function loadArtifact<T>(name: string, fallback: T): T {
  const path = join(ARTIFACT_DIR, name);
  if (!existsSync(path)) return fallback;
  return JSON.parse(readFileSync(path, 'utf8')) as T;
}

/** Merge every `prefix*.json` artifact into one record. */
export function loadMerged<T>(prefix: string): Record<string, T> {
  if (!existsSync(ARTIFACT_DIR)) return {};
  const out: Record<string, T> = {};
  for (const f of readdirSync(ARTIFACT_DIR)) {
    if (f.startsWith(prefix) && f.endsWith('.json')) {
      Object.assign(out, JSON.parse(readFileSync(join(ARTIFACT_DIR, f), 'utf8')) as Record<string, T>);
    }
  }
  return out;
}

/** Deterministic shard: item i belongs to worker i % total. */
export function shardOf<T>(items: readonly T[], index: number, total: number): T[] {
  return items.filter((_, i) => i % total === index);
}

export async function login(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login');
  await page.fill('input[name="email"]', email);
  await page.fill('input[name="password"]', password);
  await page.click('form[action="/login"] button');
  // A successful login redirects off /login; a failure re-renders it.
  await expect(page).not.toHaveURL(/\/login/, { timeout: 15_000 });
}

export async function logout(page: Page): Promise<void> {
  await page.goto('/');
  const form = page.locator('form[action="/logout"]').first();
  if (await form.count()) await form.locator('button').first().click();
}

/**
 * Submit a form and surface the app's own error text instead of a bare timeout.
 * Fervid re-renders the page with an error banner rather than redirecting when a
 * write is refused, so "did the URL change" is the honest success signal.
 */
export async function submitAndExpectNoError(page: Page, submit: () => Promise<void>): Promise<void> {
  await submit();
  await page.waitForLoadState('domcontentloaded');
  const banner = page.locator('.error, .flash-error, [role="alert"]').first();
  if (await banner.count()) {
    const text = (await banner.innerText().catch(() => '')).trim();
    if (text) throw new Error(`app refused the write: ${text}`);
  }
}

/** Map "Project / Head" -> head id, read off the budget grid's own field names. */
export async function headIdMap(page: Page, month: string): Promise<Record<string, number>> {
  await page.goto(`/budgets?month=${month}`);
  const inputs = page.locator('input[name^="budget_"]');
  const n = await inputs.count();
  const out: Record<string, number> = {};
  for (let i = 0; i < n; i++) {
    const el = inputs.nth(i);
    const name = await el.getAttribute('name');
    const label = await el.getAttribute('aria-label');
    if (!name || !label) continue;
    const id = Number(name.replace('budget_', ''));
    // aria-label is "Budget for <Project> / <Head>"
    const key = label.replace(/^Budget for\s+/, '').trim();
    if (Number.isFinite(id)) out[key] = id;
  }
  return out;
}

/** Map project name -> project id, read from the head form's project select. */
export async function projectIdMap(page: Page): Promise<Record<string, number>> {
  await page.goto('/heads');
  const options = page.locator('form.setup-form select[name="project_id"] option');
  const n = await options.count();
  const out: Record<string, number> = {};
  for (let i = 0; i < n; i++) {
    const value = await options.nth(i).getAttribute('value');
    const text = (await options.nth(i).innerText()).trim();
    const id = Number(value);
    if (Number.isFinite(id) && id > 0 && text) out[text] = id;
  }
  return out;
}

/** Map email -> user id, read from the per-user edit forms on /users. */
export async function userIdMap(page: Page): Promise<Record<string, number>> {
  await page.goto('/users');
  const rows = page.locator('form[action="/users"] input[name="id"]');
  const n = await rows.count();
  const out: Record<string, number> = {};
  for (let i = 0; i < n; i++) {
    const idAttr = await rows.nth(i).getAttribute('value');
    const id = Number(idAttr);
    if (!Number.isFinite(id)) continue;
    const form = rows.nth(i).locator('xpath=ancestor::form[1]');
    const email = await form.locator('input[name="email"]').first().getAttribute('value');
    if (email) out[email.toLowerCase()] = id;
  }
  return out;
}

/** Map vendor name -> vendor id, read from a request form's vendor select. */
export async function vendorIdMap(page: Page): Promise<Record<string, number>> {
  await page.goto('/requests/new?type=vendor_invoice');
  const options = page.locator('select[name="vendor_id"] option');
  const n = await options.count();
  const out: Record<string, number> = {};
  for (let i = 0; i < n; i++) {
    const value = await options.nth(i).getAttribute('value');
    const text = (await options.nth(i).innerText()).trim();
    const id = Number(value);
    if (Number.isFinite(id) && id > 0 && text) out[text] = id;
  }
  return out;
}

/** The numeric id of the request currently open, taken from the URL. */
export function requestIdFromUrl(url: string): number | null {
  const m = /\/requests\/(\d+)/.exec(url);
  return m ? Number(m[1]) : null;
}

/**
 * Post an action form on a request detail page. Fervid renders each action as
 * its own <form action="/requests/{id}/{verb}">, sometimes inside a disclosure,
 * so we locate by action attribute rather than by button text.
 */
export async function postRequestAction(
  page: Page,
  id: number,
  verb: string,
  fields: Record<string, string> = {},
): Promise<void> {
  await page.goto(`/requests/${id}`);
  const form = page.locator(`form[action="/requests/${id}/${verb}"]`).first();
  await expect(form, `no ${verb} form on request ${id}`).toHaveCount(1);
  for (const [name, value] of Object.entries(fields)) {
    const field = form.locator(`[name="${name}"]`).first();
    const tag = await field.evaluate((el) => el.tagName.toLowerCase()).catch(() => '');
    if (tag === 'select') await field.selectOption(value);
    else if (tag === 'input' && (await field.getAttribute('type')) === 'checkbox') {
      if (value === 'on') await field.check();
    } else await field.fill(value);
  }
  await submitAndExpectNoError(page, async () => {
    await form.locator('button[type="submit"], button:not([type="button"])').first().click();
  });
}
