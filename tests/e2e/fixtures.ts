import { expect, test as base, type Page } from '@playwright/test';

export const admin = {
  email: 'admin@fervid.local',
  password: 'admin123'
};

export const test = base.extend<{ adminPage: Page; runId: string }>({
  runId: async ({}, use) => {
    await use(`e2e-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`);
  },
  adminPage: async ({ page }, use) => {
    await login(page, admin.email, admin.password);
    await use(page);
  }
});

export { expect };

export async function login(page: Page, email: string, password: string) {
  await page.goto('/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Login' }).click();
  await expect(page).toHaveURL(/\/$/);
}

// Creating a user now happens inside the "＋ Add user" overlay sheet, so the
// sheet is opened first and every field is scoped to it: each user's own edit
// sheet also carries a Name field and a "Reset password" field, and an
// unscoped getByLabel would match several of them.
export async function createDataEntryUser(page: Page, runId: string) {
  const email = `${runId}@example.test`;
  await page.goto('/users');
  await openNewUserSheet(page);
  const sheet = page.locator('#user-new');
  await sheet.getByLabel('Email').fill(email);
  await sheet.getByLabel('Name').fill(`Data Entry ${runId}`);
  await sheet.getByLabel('Role').selectOption('data_entry');
  await sheet.getByLabel('Password').fill('StrongTestPassword!42');
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(page).toHaveURL(/\/users$/);
  return { email, password: 'StrongTestPassword!42' };
}

export async function openNewUserSheet(page: Page) {
  await page.locator('.pb-actions').getByRole('button', { name: 'Add user' }).click();
  await expect(page.locator('#user-new')).toBeVisible();
}

export async function createPayment(page: Page, runId: string, options: { amount?: string; month?: string } = {}) {
  const month = options.month ?? '2026-06';
  await page.goto(`/payments/new?month=${month}`);
  await page.getByLabel('Project / Head').selectOption({ index: 1 });
  await page.getByLabel('Paid on').fill(`${month}-15`);
  await page.getByLabel('Amount').fill(options.amount ?? '123.45');
  await page.getByLabel('Vendor / Payee').fill(`Vendor ${runId}`);
  await page.getByLabel('Payment mode').selectOption('upi');
  await page.getByLabel('Invoice / Bill No').fill(`INV-${runId}`);
  await page.getByRole('button', { name: 'Save Payment' }).click();
  await expect(page).toHaveURL(new RegExp(`\\/\\?month=${month}$`));
}

export function capturePageErrors(page: Page) {
  const failures: string[] = [];
  page.on('pageerror', error => failures.push(`pageerror: ${error.message}`));
  page.on('console', message => {
    if (message.type() === 'error') failures.push(`console: ${message.text()}`);
  });
  page.on('response', response => {
    if (response.status() >= 500) failures.push(`HTTP ${response.status()} ${response.url()}`);
  });
  return failures;
}
