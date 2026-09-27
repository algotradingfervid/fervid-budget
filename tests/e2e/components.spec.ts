import type { Page, Route } from '@playwright/test';
import { capturePageErrors, createApprovedRequest, expect, test } from './fixtures';

/**
 * Design-system behaviours (web/static/fervid-app.js).
 *
 * Most behaviours are driven against the component fixture at
 * /static/_kitchensink.html. Two of them — `[data-when]` and the submit-time
 * un-grouping of the money field — have no markup in the kitchen sink, so they
 * run against a small same-origin harness page served by route interception.
 * The harness loads the real /static/fervid-app.js, never a copy.
 */

const KITCHEN_SINK = '/static/_kitchensink.html';
const HARNESS_URL = '/static/_ds-harness.html';
const HARNESS_MATCH = /\/static\/_ds-harness\.html/;

const PHONE = { width: 390, height: 850 };

async function openHarness(page: Page, body: string) {
  await page.route(HARNESS_MATCH, (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body:
        '<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">' +
        '<title>design system harness</title></head><body>' +
        body +
        '<script src="/static/fervid-app.js" defer></script></body></html>'
    })
  );
  await page.goto(HARNESS_URL);
  await page.waitForFunction(() => document.readyState === 'complete');
}

test.describe('design system behaviours', () => {
  // The behaviours are viewport-driven and one of them resizes the window,
  // which the emulated mobile-chrome project forbids. Run the pass once.
  test.beforeEach(() => {
    test.skip(test.info().project.name !== 'chromium', 'component pass runs once');
  });

  test('payment entry rejects invalid amounts and blank references before opening its confirmation', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '100.01' });
    await adminPage.goto(`/requests/${request.id}`);
    await adminPage.getByRole('button', { name: 'Record payment', exact: true }).click();
    const amount = adminPage.getByLabel('Amount actually paid');
    const reference = adminPage.getByLabel('Transaction / UTR reference');
    const preview = adminPage.getByRole('button', { name: 'Payment settled →' });
    await adminPage.getByLabel('Paid on').fill('2026-07-20');
    await adminPage.getByLabel('Payment mode').selectOption('neft');
    await reference.fill('TEST-REF');
    for (const invalid of ['0', '-1.01', '1.001', '100.02']) {
      await amount.fill(invalid);
      await preview.click();
      await expect(adminPage.locator('.client-error-summary')).toBeVisible();
      await expect(adminPage.locator('#settle-sheet')).not.toBeVisible();
      await expect(amount).toHaveAttribute('aria-invalid', 'true');
    }
    await amount.fill('100.01');
    for (const mode of ['neft', 'rtgs', 'upi', 'cheque', 'cash', 'card', 'dd']) {
      await adminPage.getByLabel('Payment mode').selectOption(mode);
      await reference.fill('   ');
      await preview.click();
      await expect(adminPage.locator('.client-error-summary')).toContainText('transaction or payment reference');
      await expect(adminPage.locator('#settle-sheet')).not.toBeVisible();
    }
    await reference.fill('COMPONENT-VALID-REFERENCE');
    await amount.fill('50.00');
    await expect(adminPage.locator('.client-error-summary')).toHaveCount(0);
    await preview.click();
    await expect(adminPage.locator('#settle-sheet')).toBeVisible();
    await adminPage.getByRole('link', { name: 'Go back', exact: true }).click();
    await expect(reference).toHaveValue('COMPONENT-VALID-REFERENCE');
    // An unchosen shortfall in a dismissed sheet must not block a new preview.
    await amount.fill('100.01');
    await preview.click();
    await adminPage.getByRole('button', { name: 'Confirm and save payment', exact: true }).click();
    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    await expect(adminPage.getByText('COMPONENT-VALID-REFERENCE', { exact: true })).toBeVisible();
  });

  test('app validation replaces required/email popups and preserves correction and submission', async ({ page }) => {
    await page.goto('/login');
    await page.getByRole('button', { name: 'Login', exact: true }).click();
    const summary = page.locator('.client-error-summary');
    await expect(summary).toBeFocused();
    await expect(summary).toContainText('Enter Email.');
    await expect(summary).toContainText('Enter Password.');
    await expect(page.getByLabel('Email', { exact: true })).toHaveAttribute('aria-invalid', 'true');
    await page.getByLabel('Email', { exact: true }).fill('bad-address');
    await expect(summary).toContainText('Enter a valid email address, such as name@example.com.');
    await page.getByLabel('Email', { exact: true }).fill('admin@fervid.local');
    await expect(page.getByLabel('Email', { exact: true })).not.toHaveAttribute('aria-invalid', 'true');
    await page.getByLabel('Password', { exact: true }).fill('admin123');
    await expect(summary).toHaveCount(0);
    await page.getByRole('button', { name: 'Login', exact: true }).click();
    await expect(page).toHaveURL(/\/$/);
  });

  test('app validation describes bounds in real head creation without losing other input', async ({ adminPage, runId }) => {
    await adminPage.goto('/heads');
    const form = adminPage.locator('.heads-setup-form');
    await form.getByLabel('Head name', { exact: true }).fill('Bounds ' + runId);
    const due = form.getByLabel('Due day', { exact: true });
    await due.fill('32');
    await form.getByRole('button', { name: 'Add Head', exact: true }).click();
    await expect(form.locator('.client-error-summary')).toContainText('Enter Due day of no more than 31.');
    await expect(form.getByLabel('Head name', { exact: true })).toHaveValue('Bounds ' + runId);
    await due.fill('0');
    await expect(form.locator('.client-error-summary')).toContainText('Enter Due day of at least 1.');
    await due.fill('5');
    await expect(form.locator('.client-error-summary')).toHaveCount(0);
    await form.getByRole('button', { name: 'Add Head', exact: true }).click();
    await expect(adminPage.getByLabel('Head name for Bounds ' + runId, { exact: true })).toHaveValue('Bounds ' + runId);
  });

  test('app validation keeps password length errors inside the user dialog', async ({ adminPage }) => {
    await adminPage.goto('/users');
    await adminPage.locator('.pb-actions').getByRole('button', { name: 'Add user' }).click();
    const sheet = adminPage.locator('#user-new');
    await sheet.getByLabel('Email', { exact: true }).fill('validation-only@example.test');
    await sheet.getByLabel('Name', { exact: true }).fill('Validation only');
    await sheet.getByLabel('Password', { exact: true }).pressSequentially('short1');
    await sheet.getByRole('button', { name: 'Add User', exact: true }).click();
    await expect(sheet.locator('.client-error-summary')).toContainText('Use at least 12 characters for Password.');
    await expect(sheet.locator('.client-error-summary')).toBeFocused();
    await sheet.getByLabel('Password', { exact: true }).fill('LongEnoughPassword123');
    await expect(sheet.locator('.client-error-summary')).toHaveCount(0);
  });

  test('app validation covers request date and selection requirements after dynamic type rendering', async ({ adminPage }) => {
    await adminPage.goto('/requests/new?type=reimbursement');
    await adminPage.getByRole('button', { name: 'Submit request', exact: true }).click();
    const summary = adminPage.locator('.client-error-summary');
    await expect(summary).toContainText('Choose Approver.');
    await expect(summary).toContainText('Enter Expense date.');
    await expect(summary).toBeFocused();
    await adminPage.getByRole('link', { name: 'Enter Expense date.', exact: true }).click();
    await expect(adminPage.locator('#expense-date')).toBeFocused();
  });

  test('app validation requires urgency details only while urgent and preserves other errors', async ({ adminPage }) => {
    await adminPage.goto('/requests/new?type=reimbursement');
    const urgent = adminPage.getByRole('checkbox', { name: 'Mark this urgent', exact: true });
    const reason = adminPage.locator('#urgency-reason');
    await expect(reason).not.toHaveAttribute('required', '');
    await urgent.check();
    await expect(reason).toHaveAttribute('required', '');
    await adminPage.getByRole('button', { name: 'Submit request', exact: true }).click();
    const summary = adminPage.locator('.client-error-summary');
    await expect(summary).toContainText('Explain why this is urgent.');
    await expect(summary).toContainText('Enter Short title.');
    await urgent.uncheck();
    await expect(reason).toBeHidden();
    await expect(reason).not.toHaveAttribute('required', '');
    await expect(reason).not.toHaveAttribute('aria-invalid', 'true');
    await expect(summary).not.toContainText('Explain why this is urgent.');
    await expect(summary).toContainText('Enter Short title.');
    await urgent.check();
    await adminPage.getByRole('button', { name: 'Submit request', exact: true }).click();
    await expect(summary).toContainText('Explain why this is urgent.');
    await reason.fill('Critical supplies are needed tomorrow.');
    await expect(summary).not.toContainText('Explain why this is urgent.');
    await expect(summary).toContainText('Enter Short title.');
  });

  test('app validation clears only tagged urgency fallback errors and improves correction wording', async ({ page }) => {
    await openHarness(page, `<div class="alert error" data-server-error-for="urgency_reason">say why this is urgent</div>
      <div class="alert error" id="other-error">A different error must remain.</div>
      <form><label><input name="urgent" type="checkbox" checked>Marked urgent</label>
      <div data-when="urgent:on"><label>Why is it urgent<input name="urgency_reason" data-required-when-visible></label></div>
      <label>What needs correcting<textarea required></textarea></label><button>Return request</button></form>`);
    await page.getByRole('checkbox', { name: 'Marked urgent' }).uncheck();
    await expect(page.locator('[data-server-error-for="urgency_reason"]')).toHaveCount(0);
    await expect(page.locator('#other-error')).toBeVisible();
    await page.getByRole('button', { name: 'Return request' }).click();
    await expect(page.locator('.client-error-summary')).toContainText('Explain what needs correcting.');
    await expect(page.locator('#other-error')).toBeVisible();
  });

  test('app validation handles linked rows, radio groups, disabled fields and dynamic forms', async ({ page }) => {
    await openHarness(page, `
      <form id="row"></form>
      <label>Row name<input form="row" name="name" required aria-describedby="row-help"></label>
      <span id="row-help">Keep the existing name until you save.</span>
      <button form="row">Save row</button>
      <form id="other"><label>Unrelated<input required></label></form>
      <form id="choices"><fieldset><legend>Settlement</legend>
        <label><input type="radio" name="settlement" value="a" required>Installment</label>
        <label><input type="radio" name="settlement" value="b">Settled</label></fieldset>
        <input name="unavailable" required disabled><input name="optional" hidden>
        <button>Save choices</button></form>`);
    await page.getByRole('button', { name: 'Save row' }).click();
    await expect(page.locator('.client-error-summary')).toHaveCount(1);
    await expect(page.locator('#other .client-field-error')).toHaveCount(0);
    await page.getByRole('link', { name: 'Enter Row name.' }).click();
    await expect(page.getByLabel('Row name')).toBeFocused();
    await expect(page.getByLabel('Row name')).toHaveAttribute('aria-describedby', /row-help validation-message-/);
    await page.getByLabel('Row name').fill('Retained');
    await expect(page.getByLabel('Row name')).toHaveAttribute('aria-describedby', 'row-help');
    await page.getByRole('button', { name: 'Save choices' }).click();
    await expect(page.locator('#choices .client-error-summary li')).toHaveCount(1);
    await expect(page.locator('#choices .client-field-error')).toHaveCount(1);
    await page.getByLabel('Settled', { exact: true }).check();
    await expect(page.locator('.client-error-summary')).toHaveCount(0);
    // Delegated invalid handling must also cover HTMX-inserted forms.
    await page.evaluate(() => {
      const form = document.createElement('form');
      form.innerHTML = '<label>Code<input required pattern="[A-Z]{3}" title="Use three uppercase letters."></label><button>Save code</button>';
      document.body.appendChild(form);
    });
    await page.getByLabel('Code').fill('abc');
    await page.getByRole('button', { name: 'Save code' }).click();
    await expect(page.locator('.client-error-summary')).toContainText('Use three uppercase letters.');
    await page.getByLabel('Code').fill('ABC');
    await expect(page.locator('.client-error-summary')).toHaveCount(0);
    await page.evaluate(() => {
      const form = document.createElement('form');
      form.id = 'conditional';
      form.innerHTML = '<label>Include detail<select><option>Yes</option><option>No</option></select></label><div id="conditional-fields"><label>Detail<input required></label></div><button>Save detail</button>';
      document.body.appendChild(form);
      // Register later than shared JS, as dynamically swapped form scripts do.
      document.addEventListener('change', () => {
        const select = form.querySelector('select')!;
        const input = form.querySelector('input');
        if (input) input.disabled = select.value === 'No';
      });
    });
    await page.getByRole('button', { name: 'Save detail' }).click();
    await expect(page.locator('#conditional .client-error-summary')).toContainText('Enter Detail.');
    await page.getByLabel('Include detail').selectOption('No');
    await expect(page.locator('#conditional .client-error-summary')).toHaveCount(0);
    await expect(page.getByLabel('Detail', { exact: true })).not.toHaveAttribute('aria-invalid', 'true');
    await page.getByLabel('Include detail').selectOption('Yes');
    await page.getByRole('button', { name: 'Save detail' }).click();
    await expect(page.locator('#conditional .client-error-summary')).toBeVisible();
    await page.evaluate(() => {
      document.getElementById('conditional-fields')!.replaceChildren();
      document.dispatchEvent(new Event('htmx:afterSwap'));
      document.dispatchEvent(new Event('htmx:afterSettle'));
    });
    await expect(page.locator('#conditional .client-error-summary')).toHaveCount(0);
  });

  /* ------------------------------------------------------------------ */
  /* 1. Accordion                                                        */
  /* ------------------------------------------------------------------ */

  test('.acc-head toggles its .acc-item with aria-expanded and hidden', async ({ page }) => {
    const errors = capturePageErrors(page);
    await page.goto(KITCHEN_SINK);

    const items = page.locator('.acc > .acc-item');
    const openHead = items.nth(0).locator('.acc-head');
    const openBody = items.nth(0).locator('.acc-body');
    const shutHead = items.nth(1).locator('.acc-head');
    const shutBody = items.nth(1).locator('.acc-body');

    // The markup ships the first item open and the second closed; the script
    // must publish that state through the button, not through a class alone.
    await expect(openHead).toHaveAttribute('aria-expanded', 'true');
    await expect(openBody).toBeVisible();
    await expect(shutHead).toHaveAttribute('aria-expanded', 'false');
    await expect(shutBody).toBeHidden();
    expect(await shutBody.evaluate(el => (el as HTMLElement).hidden)).toBe(true);

    await openHead.click();
    await expect(openHead).toHaveAttribute('aria-expanded', 'false');
    await expect(openBody).toBeHidden();
    expect(await openBody.evaluate(el => (el as HTMLElement).hidden)).toBe(true);
    await expect(items.nth(0)).not.toHaveClass(/is-open/);

    await openHead.click();
    await expect(openHead).toHaveAttribute('aria-expanded', 'true');
    await expect(openBody).toBeVisible();
    await expect(items.nth(0)).toHaveClass(/is-open/);

    await shutHead.click();
    await expect(shutHead).toHaveAttribute('aria-expanded', 'true');
    await expect(shutBody).toBeVisible();

    expect(errors).toEqual([]);
  });

  test('.pa-head toggles the permission accordion', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const head = page.locator('.perm-acc .pa-head').first();
    const body = page.locator('.perm-acc .pa-body').first();

    await expect(head).toHaveAttribute('aria-expanded', 'true');
    await expect(body).toBeVisible();

    await head.click();
    await expect(head).toHaveAttribute('aria-expanded', 'false');
    await expect(body).toBeHidden();

    await head.click();
    await expect(head).toHaveAttribute('aria-expanded', 'true');
    await expect(body).toBeVisible();
  });

  /* ------------------------------------------------------------------ */
  /* 2. Overlay / sheet                                                  */
  /* ------------------------------------------------------------------ */

  test('[data-open] opens the overlay and moves focus into the sheet', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const overlay = page.locator('#ks-overlay');
    await expect(overlay).toBeHidden();

    await page.getByRole('button', { name: 'Open overlay' }).click();
    await expect(overlay).toBeVisible();
    await expect(page.locator('#ks-overlay .sh-close')).toBeFocused();
  });

  test('[data-close] closes the overlay and restores focus to the opener', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const opener = page.getByRole('button', { name: 'Open overlay' });
    const overlay = page.locator('#ks-overlay');

    await opener.click();
    await expect(overlay).toBeVisible();

    await page.locator('#ks-overlay .sh-foot').getByRole('button', { name: 'Cancel' }).click();
    await expect(overlay).toBeHidden();
    await expect(opener).toBeFocused();
  });

  test('clicking the overlay backdrop closes the sheet', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const opener = page.getByRole('button', { name: 'Open overlay' });
    const overlay = page.locator('#ks-overlay');

    await opener.click();
    await expect(overlay).toBeVisible();

    // Top-left corner is backdrop: .overlay centres the sheet with 20px padding.
    await overlay.click({ position: { x: 4, y: 4 } });
    await expect(overlay).toBeHidden();
    await expect(opener).toBeFocused();
  });

  test('Escape closes the open sheet', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const opener = page.getByRole('button', { name: 'Open overlay' });
    const overlay = page.locator('#ks-overlay');

    await opener.click();
    await expect(overlay).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(overlay).toBeHidden();
    await expect(opener).toBeFocused();
  });

  test('focus is trapped inside the open sheet', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    await page.getByRole('button', { name: 'Open overlay' }).click();

    const close = page.locator('#ks-overlay .sh-close');
    const confirm = page.locator('#ks-overlay .sh-foot').getByRole('button', { name: 'Confirm' });

    await expect(close).toBeFocused();

    // Backwards off the first focusable must wrap to the last, not escape.
    await page.keyboard.press('Shift+Tab');
    await expect(confirm).toBeFocused();

    // Forwards off the last must wrap back to the first.
    await page.keyboard.press('Tab');
    await expect(close).toBeFocused();

    // And it never leaks out however long you tab.
    for (let i = 0; i < 8; i += 1) await page.keyboard.press('Tab');
    expect(
      await page.evaluate(() => !!document.getElementById('ks-overlay')?.contains(document.activeElement))
    ).toBe(true);
  });

  /* ------------------------------------------------------------------ */
  /* 3. More sheet                                                       */
  /* ------------------------------------------------------------------ */

  test('mobile navigation exposes a named modal and restores the background on close', async ({ adminPage }) => {
    await adminPage.setViewportSize(PHONE);
    await adminPage.goto('/');
    const tab = adminPage.locator('.tabbar .js-more');
    await tab.click();
    const dialog = adminPage.getByRole('dialog', { name: 'More navigation' });
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute('aria-modal', 'true');
    await expect(tab).toHaveAttribute('aria-expanded', 'true');
    expect(await adminPage.locator('main').evaluate(el => (el as HTMLElement).inert)).toBe(true);
    expect(await adminPage.locator('.tabbar').evaluate(el => (el as HTMLElement).inert)).toBe(true);
    await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeFocused();
    await adminPage.keyboard.press('Shift+Tab');
    await expect(dialog.getByRole('button', { name: /Log out/ })).toBeFocused();
    await adminPage.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(tab).toBeFocused();
    await expect(tab).toHaveAttribute('aria-expanded', 'false');
    expect(await adminPage.locator('main').evaluate(el => (el as HTMLElement).inert)).toBe(false);
    expect(await adminPage.evaluate(() => document.body.style.overflow)).toBe('');
    await tab.click();
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(tab).toBeFocused();
  });

  test('admin and request filter sheets expose their visible title and Close action', async ({ adminPage }) => {
    for (const scenario of [
      { route: '/users', opener: '[data-open="user-new"]', title: 'Add user' },
      { route: '/roles', opener: '[data-open="role-new"]', title: 'New role' },
      { route: '/admin/notifications', opener: '[data-open="ev-request_submitted"]', title: 'Request submitted' },
      { route: '/requests', opener: '[data-open="filter-sheet"]', title: 'Filters' }
    ]) {
      await adminPage.setViewportSize(PHONE);
      await adminPage.goto(scenario.route);
      const opener = adminPage.locator(scenario.opener).filter({ visible: true }).first();
      await opener.click();
      const dialog = adminPage.getByRole('dialog', { name: scenario.title, exact: true });
      await expect(dialog).toBeVisible();
      await expect(dialog).toHaveAttribute('aria-modal', 'true');
      await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeFocused();
      await adminPage.keyboard.press('Escape');
      await expect(dialog).toBeHidden();
      await expect(opener).toBeFocused();
      expect(await adminPage.locator('[inert]').count()).toBe(0);
    }
  });

  test('mobile request actions stay above navigation throughout the form', async ({ adminPage }) => {
    for (const width of [390, 320, 820]) {
      await adminPage.setViewportSize({ width, height: 844 });
      await adminPage.goto('/requests/new?type=vendor_invoice');
      for (const fraction of [0, 0.5, 1]) {
        const actions = await adminPage.evaluate(fraction => {
          window.scrollTo(0, (document.documentElement.scrollHeight - innerHeight) * fraction);
          return Array.from(document.querySelectorAll('.action-bar a, .action-bar button')).map(el => {
            const r = el.getBoundingClientRect();
            const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
            return { label: el.textContent, reachable: !!hit && el.contains(hit) };
          });
        }, fraction);
        expect(actions.length).toBeGreaterThanOrEqual(2);
        for (const action of actions) expect(action.reachable, `${width}px at ${fraction}: ${action.label}`).toBe(true);
      }
      await adminPage.getByLabel('Short title').focus();
      for (let step = 0; step < 10; step++) {
        await adminPage.keyboard.press('Tab');
        await expect.poll(() => adminPage.evaluate(() => {
          const el = document.activeElement;
          if (!el || !el.matches('main input, main select, main textarea')) return true;
          const r = el.getBoundingClientRect();
          const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
          return !!hit && el.contains(hit);
        }), { message: `Focused field visible at ${width}px, tab ${step}` }).toBe(true);
      }
    }
  });

  test('the More tab opens the more sheet and .ms-close dismisses it', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await page.goto(KITCHEN_SINK);

    const more = page.locator('.more-sheet');
    const tab = page.locator('.tabbar .js-more');

    await expect(more).toBeHidden();
    await expect(tab).toBeVisible();

    await tab.click();
    await expect(more).toBeVisible();

    await page.locator('.more-sheet .ms-close').click();
    await expect(more).toBeHidden();
    await expect(tab).toBeFocused();
  });

  /* ------------------------------------------------------------------ */
  /* 4. [data-when] conditional reveal                                   */
  /* ------------------------------------------------------------------ */

  const CONDITIONAL_FORM = `
    <form id="hf" method="get" action="${HARNESS_URL}">
      <select name="mode" aria-label="Mode">
        <option value="upi">UPI</option>
        <option value="neft">NEFT</option>
        <option value="rtgs">RTGS</option>
        <option value="cash">Cash</option>
      </select>
      <div id="w-bank" data-when="mode:neft|rtgs">Bank block</div>

      <label><input type="radio" name="kind" value="advance" checked> Advance</label>
      <label><input type="radio" name="kind" value="invoice"> Invoice</label>
      <div id="w-invoice" data-when="kind:invoice">Invoice block</div>

      <label><input type="checkbox" name="recover"> Recoverable</label>
      <div id="w-recover" data-when="recover:on">Recover block</div>
      <div id="w-not-recover" data-when="recover:off">Not-recoverable block</div>
    </form>`;

  test('[data-when] reveals on a select value', async ({ page }) => {
    await openHarness(page, CONDITIONAL_FORM);
    const bank = page.locator('#w-bank');

    await expect(bank).toBeHidden();
    await page.getByLabel('Mode').selectOption('neft');
    await expect(bank).toBeVisible();

    // The `|` alternation must match every listed value.
    await page.getByLabel('Mode').selectOption('rtgs');
    await expect(bank).toBeVisible();

    await page.getByLabel('Mode').selectOption('cash');
    await expect(bank).toBeHidden();
  });

  test('[data-when] reveals on a radio group value', async ({ page }) => {
    await openHarness(page, CONDITIONAL_FORM);
    const invoice = page.locator('#w-invoice');

    await expect(invoice).toBeHidden();
    await page.locator('input[name="kind"][value="invoice"]').check();
    await expect(invoice).toBeVisible();

    await page.locator('input[name="kind"][value="advance"]').check();
    await expect(invoice).toBeHidden();
  });

  test('[data-when] maps a checkbox to on and off', async ({ page }) => {
    await openHarness(page, CONDITIONAL_FORM);
    const on = page.locator('#w-recover');
    const off = page.locator('#w-not-recover');

    await expect(on).toBeHidden();
    await expect(off).toBeVisible();

    await page.locator('input[name="recover"]').check();
    await expect(on).toBeVisible();
    await expect(off).toBeHidden();

    await page.locator('input[name="recover"]').uncheck();
    await expect(on).toBeHidden();
    await expect(off).toBeVisible();
  });

  /* ------------------------------------------------------------------ */
  /* 5. Money field                                                      */
  /* ------------------------------------------------------------------ */

  test('the money field groups digits in the Indian system as you type', async ({ page }) => {
    await page.goto(KITCHEN_SINK);
    const money = page.locator('#ks-money');

    await money.fill('');
    await money.pressSequentially('1234567');
    // Typed one key at a time — wrong caret handling scrambles the digits.
    await expect(money).toHaveValue('12,34,567');

    await money.fill('');
    await money.pressSequentially('250000.5');
    await expect(money).toHaveValue('2,50,000.5');
  });

  test('the money field spells the amount into .in-words', async ({ page }) => {
    await page.goto(KITCHEN_SINK);
    const money = page.locator('#ks-money');
    const words = page.locator('.money-field .in-words');

    // Ships pre-filled at 1,00,000 — the script must reconcile on load.
    await expect(words).toHaveText('One lakh rupees only');

    await money.fill('250000');
    await expect(words).toHaveText('Two lakh fifty thousand rupees only');

    await money.fill('12345678');
    await expect(money).toHaveValue('1,23,45,678');
    await expect(words).toHaveText('One crore twenty three lakh forty five thousand six hundred seventy eight rupees only');

    // Fractional amounts must describe the exact paise visible in the input.
    for (const [amount, expected] of [
      ['400.13', 'Four hundred rupees and thirteen paise only'],
      ['123.45', 'One hundred twenty three rupees and forty five paise only'],
      ['0.01', 'Zero rupees and one paisa only'],
      ['.99', 'Zero rupees and ninety nine paise only'],
      ['1.1', 'One rupee and ten paise only'],
      ['1.00', 'One rupee only'],
      ['0', 'Enter an amount greater than zero.'],
      ['1234567.45', 'Twelve lakh thirty four thousand five hundred sixty seven rupees and forty five paise only'],
      ['10000000000.01', 'One thousand crore rupees and one paisa only'],
      ['9007199254740993.99', 'Amount too large to display in words'],
    ]) {
      await money.fill(amount);
      await expect(words).toHaveText(expected);
    }

    await money.fill('');
    await expect(words).toHaveText('Enter the amount you are requesting');
    await expect(words).toHaveClass(/empty/);
  });

  test('the money field posts ungrouped digits on submit', async ({ page }) => {
    await openHarness(
      page,
      `<form id="mf" method="get" action="${HARNESS_URL}">
         <div class="money-field">
           <label for="amt">Amount</label>
           <input id="amt" name="amount" value="">
           <span class="in-words"></span>
         </div>
         <button type="submit">Save</button>
       </form>`
    );

    const money = page.locator('#amt');
    await money.fill('250000');
    await expect(money).toHaveValue('2,50,000');

    await Promise.all([
      page.waitForURL(/_ds-harness\.html\?/),
      page.getByRole('button', { name: 'Save' }).click()
    ]);

    // Commas must be gone before the request leaves the browser.
    expect(new URL(page.url()).searchParams.get('amount')).toBe('250000');
  });

  /* ------------------------------------------------------------------ */
  /* 6. Retained toggleProject                                           */
  /* ------------------------------------------------------------------ */

  test('toggleProject still collapses the variance grid', async ({ adminPage }) => {
    await adminPage.goto('/grid');

    const toggle = adminPage.locator('[data-project-toggle]').first();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');

    const projectID = await toggle.getAttribute('data-project-toggle');
    const rows = adminPage.locator(`[data-project-row="${projectID}"]`);
    expect(await rows.count()).toBeGreaterThan(0);
    await expect(rows.first()).toBeVisible();

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(rows.first()).toBeHidden();
    await expect(adminPage.locator('.project-row').first()).toHaveClass(/is-collapsed/);

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(rows.first()).toBeVisible();
  });
});
