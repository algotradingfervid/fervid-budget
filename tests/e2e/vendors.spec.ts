import type { Page } from '@playwright/test';
import { expect, test, login, createDataEntryUser } from './fixtures';

/**
 * Phase 1V — the vendor master.
 *
 * The second test is the one that matters: it proves a session without
 * vendor_bank:view cannot see bank details on the detail screen. The Go tests
 * prove the store never returns them; this proves the screen a real person
 * looks at never shows them either.
 */

const IFSC = 'HDFC0000521';
const ACCOUNT = '50200041294471';

async function isMobile(page: Page) {
  return page.viewportSize()!.width <= 860;
}

// Granting through the roles matrix, which renders twice — a desktop table and
// a mobile accordion, with fervid-app.js disabling whichever does not belong at
// that width. Ticking the wrong one submits nothing, so the helper picks by
// viewport exactly as a person would.
async function grantVendorViewToAccounts(page: Page) {
  await page.goto('/roles');
  await page.locator('.segmented a', { hasText: 'Accounts' }).first().click();
  await expect(page.locator('#role-form input[name="name"]')).toHaveValue('Accounts');

  if (await isMobile(page)) {
    const row = page.locator('.perm-acc .pa-item').filter({
      has: page.getByText('Vendors', { exact: true })
    });
    await row.locator('.pa-head').click();
    await row.locator('input[name="cell"][value="vendors:view"]').check();
  } else {
    await page.locator('.perm-table input[name="cell"][value="vendors:view"]').check();
  }
  await page.getByRole('button', { name: 'Save role' }).click();
  await expect(page).toHaveURL(/\/roles\?role=\d+$/);
}

async function createVendor(page: Page, name: string) {
  await page.goto('/vendors/new');
  await page.getByLabel('Vendor name').fill(name);
  await page.getByLabel('Short name').fill('Sundaram Elec');
  await page.getByLabel('Type').selectOption('company');
  await page.getByLabel('Categories').fill('Materials, Switchgear');
  await page.getByLabel('GSTIN').fill('29AABCS1429B1ZQ');
  await page.getByLabel('Contact person').fill('R. Subramanian');
  await page.getByLabel('City', { exact: true }).fill('Bengaluru');
  await page.getByLabel('Account number').fill(ACCOUNT);
  await page.getByLabel('IFSC').fill(IFSC);
  await page.getByRole('button', { name: 'Save vendor' }).click();
  await expect(page).toHaveURL(/\/vendors\/\d+$/);
}

test.describe('vendor master', () => {
  test('admin creates a vendor, finds it in the list and edits it', async ({ adminPage, runId }) => {
    const name = `Sundaram Electricals ${runId}`;
    await createVendor(adminPage, name);

    // The record round-trips.
    await expect(adminPage.getByLabel('Vendor name')).toHaveValue(name);
    await expect(adminPage.getByLabel('IFSC')).toHaveValue(IFSC);

    // It is in the list, with its categories read back as a · chain.
    await adminPage.goto('/vendors');
    const row = adminPage.locator('table.t-cards tbody tr').filter({ hasText: name });
    await expect(row).toHaveCount(1);
    await expect(row).toContainText('Materials · Switchgear');
    await expect(row).toContainText('Bengaluru');

    // Below 860px the table restacks into cards: the header row goes away and
    // each cell is captioned by its data-label instead.
    const mobile = await isMobile(adminPage);
    await expect(adminPage.locator('table.t-cards thead')).toBeVisible({ visible: !mobile });

    // Open it from the list and edit the contact person.
    await row.getByRole('link', { name }).click();
    await expect(adminPage).toHaveURL(/\/vendors\/\d+$/);
    await adminPage.getByLabel('Contact person').fill('B. Krishnan');
    await adminPage.getByRole('button', { name: 'Save vendor' }).click();
    await expect(adminPage).toHaveURL(/\/vendors\/\d+$/);
    await expect(adminPage.getByLabel('Contact person')).toHaveValue('B. Krishnan');

    // Nothing the user must operate may sit under the fixed tab bar. The click
    // above already proves it: Playwright refuses to click an element another
    // element covers. This measures the other half of the contract — the page's
    // reserved bottom band — at the one place it has to hold, the very end of
    // the document, where the action bar lives.
    await adminPage.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    const overlap = await adminPage.evaluate(() => {
      const bar = document.querySelector('.action-bar');
      const tabs = document.querySelector('.tabbar');
      if (!bar || !tabs) return 0;
      const style = getComputedStyle(tabs);
      if (style.display === 'none' || style.visibility === 'hidden') return 0;
      const a = bar.getBoundingClientRect();
      const b = tabs.getBoundingClientRect();
      const vertical = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
      const horizontal = Math.min(a.right, b.right) - Math.max(a.left, b.left);
      return vertical > 0 && horizontal > 0 ? vertical : 0;
    });
    expect(overlap, 'tab bar overlapping the action bar, in px').toBe(0);
  });

  test('a session without the bank permission cannot see bank details', async ({ adminPage, runId }) => {
    const name = `Meridian Facility ${runId}`;
    await createVendor(adminPage, name);
    await grantVendorViewToAccounts(adminPage);
    const user = await createDataEntryUser(adminPage, runId);

    // Same browser, different person: the data-entry user holds vendor:view
    // through the Accounts role and no vendor_bank permission at all.
    await adminPage.context().clearCookies();
    await login(adminPage, user.email, user.password);

    await adminPage.goto('/vendors');
    const row = adminPage.locator('table.t-cards tbody tr').filter({ hasText: name });
    await expect(row).toHaveCount(1);
    await row.getByRole('link', { name }).click();
    await expect(adminPage).toHaveURL(/\/vendors\/\d+$/);

    // The record is readable…
    await expect(adminPage.getByLabel('GSTIN')).toHaveValue('29AABCS1429B1ZQ');
    // …and told plainly that the bank block is withheld, not absent.
    await expect(adminPage.locator('.banner.locked')).toContainText(
      'You do not have permission to see bank details'
    );

    // The proof: the values are not in the document at all, and there is no
    // field to put them in. Not hidden — never sent.
    const markup = await adminPage.content();
    expect(markup).not.toContain(IFSC);
    expect(markup).not.toContain(ACCOUNT);
    expect(markup).not.toContain('bank_ifsc');
    await expect(adminPage.locator('input[name="bank_ifsc"]')).toHaveCount(0);

    // Read-only: no save, but a way back.
    await expect(adminPage.getByRole('button', { name: 'Save vendor' })).toHaveCount(0);
    await expect(adminPage.locator('.action-bar').getByRole('link', { name: 'Back to vendors' })).toBeVisible();
  });
});
