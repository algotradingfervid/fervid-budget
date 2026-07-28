import { test, expect } from '@playwright/test';
import { login, headIdMap } from './helpers';
import { MONTHS, projects, budgets } from './plan';

// Phase 3: open each month, then fill its budget grid.
//
// The grid is one form per month carrying an input per head (`budget_<headID>`),
// so a month is a single submit rather than 229 of them. Months are independent,
// so each gets its own worker.

const ADMIN = { email: 'admin@fervid.local', password: 'admin123' };

test.describe.configure({ mode: 'parallel' });

const labelFor = new Map<string, string>();
for (const p of projects) for (const h of p.heads) labelFor.set(h.key, `${p.name} / ${h.name}`);

for (const month of MONTHS) {
  test(`month ${month}: create and budget`, async ({ page }) => {
    await login(page, ADMIN.email, ADMIN.password);

    // Open the month. Already-existing months are fine — 2026-06 and 2026-07
    // were already there before this run.
    await page.goto('/months');
    const create = page.locator('form[action="/months"]').first();
    if (await create.count()) {
      await create.locator('input[name="target_month"]').fill(month);
      const mode = create.locator('select[name="source_mode"]');
      if (await mode.count()) await mode.selectOption('blank');
      await create.locator('button[type="submit"], button.primary').first().click();
      await page.waitForLoadState('domcontentloaded');
    }

    const heads = await headIdMap(page, month);
    expect(Object.keys(heads).length, `no budget inputs for ${month}`).toBeGreaterThan(0);

    await page.goto(`/budgets?month=${month}`);
    const form = page.locator('form.budget-editor').first();
    await expect(form, `no budget editor for ${month}`).toHaveCount(1);

    let filled = 0;
    for (const b of budgets.filter((x) => x.month === month)) {
      const label = labelFor.get(b.headKey);
      if (!label) continue;
      const id = heads[label];
      if (!id) continue;
      const field = form.locator(`input[name="budget_${id}"]`);
      if (!(await field.count())) continue;
      await field.fill(String(b.amount));
      filled++;
    }
    expect(filled, `nothing to fill for ${month}`).toBeGreaterThan(200);

    await form.locator('button[type="submit"], button.primary').first().click();
    await page.waitForLoadState('domcontentloaded');

    const bad = page.locator('.banner.bad, .error').first();
    if (await bad.count()) {
      const text = (await bad.innerText().catch(() => '')).trim();
      if (text) throw new Error(`budgets for ${month}: ${text.slice(0, 300)}`);
    }
  });
}
