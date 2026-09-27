/* ==========================================================================
   Fervid Budget — page behaviour
   Adds the interactive parts to the server-rendered pages: accordions,
   dialogs and sheets (with a focus trap), show/hide by field value
   (`data-when`), the rupee amount field, the app's own form-error messages,
   the roles matrix, and the variance-grid project toggle.

   Everything here is progressive enhancement. Nothing decides what a user is
   allowed to see or do: the server settles that in the template and
   re-validates every submitted field. `hidden` here is presentation, never a
   security boundary.

   The login page loads this file without htmx, so nothing here may assume
   htmx is present.
   ========================================================================== */

(() => {
  "use strict";

  const EMPTY_WORDS = "Enter the amount you are requesting";

  /* ---------------------------------------------------------------- */
  /* Variance grid — collapse and expand a project's rows              */
  /* ---------------------------------------------------------------- */

  function toggleProject(button) {
    const projectID = button.getAttribute("data-project-toggle");
    const table = button.closest("table");
    if (!projectID || !table) return;

    const isExpanded = button.getAttribute("aria-expanded") !== "false";
    for (const row of table.querySelectorAll(`[data-project-row="${projectID}"]`)) {
      row.hidden = isExpanded;
    }

    button.setAttribute("aria-expanded", isExpanded ? "false" : "true");
    const projectRow = button.closest(".project-row");
    if (projectRow) projectRow.classList.toggle("is-collapsed", isExpanded);
  }

  /* ---------------------------------------------------------------- */
  /* Accordions — .acc-head/.acc-item and .pa-head/.pa-item            */
  /* ---------------------------------------------------------------- */

  const HEAD_SELECTOR = ".acc-head, .pa-head";
  let uid = 0;

  function accParts(head) {
    const item = head.closest(".acc-item, .pa-item");
    if (!item) return null;
    const body = item.querySelector(".acc-body, .pa-body");
    if (!body) return null;
    return { item, body };
  }

  function setAccordion(head, open) {
    const parts = accParts(head);
    if (!parts) return;
    parts.item.classList.toggle("is-open", open);
    parts.body.hidden = !open;
    head.setAttribute("aria-expanded", open ? "true" : "false");
    if (!parts.body.id) {
      uid += 1;
      parts.body.id = `acc-body-${uid}`;
    }
    head.setAttribute("aria-controls", parts.body.id);
  }

  /* Accordion heads are always <button type="button">. The server marks an
     open item with `is-open`; this copies that state onto the button
     (aria-expanded) and the body (hidden) so assistive tech and CSS agree. */
  function initAccordions(root) {
    for (const head of root.querySelectorAll(HEAD_SELECTOR)) {
      const parts = accParts(head);
      if (parts) setAccordion(head, parts.item.classList.contains("is-open"));
    }
  }

  function toggleAccordion(head) {
    setAccordion(head, head.getAttribute("aria-expanded") === "false");
  }

  /* ---------------------------------------------------------------- */
  /* Dialogs, sheets and the mobile More sheet                         */
  /* ---------------------------------------------------------------- */

  const FOCUSABLE = [
    "a[href]",
    "button:not([disabled])",
    "input:not([disabled]):not([type='hidden'])",
    "select:not([disabled])",
    "textarea:not([disabled])",
    "[tabindex]:not([tabindex='-1'])"
  ].join(",");

  /* Open dialogs form a stack, so a sheet opened from a sheet closes first. */
  const openDialogs = [];
  let dialogBackground = [];
  let dialogBodyOverflow = null;
  let dialogTitleCounter = 0;

  /* Makes everything outside the top dialog inert and stops the page behind
     it scrolling; undoes both when the last dialog closes. */
  function syncDialogBackground() {
    for (const node of dialogBackground) node.inert = false;
    dialogBackground = [];
    const dialog = topDialog();
    if (!dialog) {
      if (dialogBodyOverflow !== null) document.body.style.overflow = dialogBodyOverflow;
      dialogBodyOverflow = null;
      return;
    }
    if (dialogBodyOverflow === null) dialogBodyOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    // Recompute from the top sheet so nested dialogs do not leave their own
    // ancestors inert. Nodes that were already inert are left alone.
    for (let branch = dialog; branch.parentElement; branch = branch.parentElement) {
      for (const sibling of branch.parentElement.children) {
        if (sibling !== branch && !sibling.inert) {
          sibling.inert = true;
          dialogBackground.push(sibling);
        }
      }
      if (branch.parentElement === document.body) break;
    }
  }

  /* Focusable elements inside root that are actually on screen. */
  function focusables(root) {
    return Array.from(root.querySelectorAll(FOCUSABLE)).filter((node) => node.getClientRects().length > 0);
  }

  function dialogIndex(el) {
    for (let i = openDialogs.length - 1; i >= 0; i -= 1) {
      if (openDialogs[i].el === el) return i;
    }
    return -1;
  }

  /**
   * Fills the approver <select>s inside a panel when it opens, copying the
   * options from the page's single <template id="approver-options"> so the
   * list is sent once rather than once per person. The panel's own user is
   * left out: nobody may approve their own requests.
   */
  function fillApproverPicker(root) {
    const source = document.getElementById("approver-options");
    if (!source) return;
    for (const sel of root.querySelectorAll("select[data-approver-select]")) {
      if (sel.dataset.filled === "1") continue;
      const self = sel.getAttribute("data-self");
      const chosen = sel.getAttribute("data-chosen") || "0";
      for (const opt of source.content.cloneNode(true).querySelectorAll("option")) {
        if (opt.value === self) continue;
        if (opt.value === chosen) opt.selected = true;
        sel.appendChild(opt);
      }
      sel.dataset.filled = "1";
    }
  }

  /**
   * Fills a row's project <select> the first time someone reaches for it.
   * Each row ships only its current project; the full list comes from the
   * page's single <template id="project-options">.
   */
  function fillProjectPicker(sel) {
    if (!sel || sel.dataset.filled === "1") return;
    const source = document.getElementById("project-options");
    if (!source) return;
    const chosen = sel.getAttribute("data-chosen");
    sel.innerHTML = "";
    for (const opt of source.content.cloneNode(true).querySelectorAll("option")) {
      if (opt.value === chosen) opt.selected = true;
      sel.appendChild(opt);
    }
    sel.dataset.filled = "1";
  }

  // pointerdown as well as focusin: a mouse click opens the native dropdown,
  // and pointerdown is the last moment before it does.
  function projectPickerFrom(target) {
    return target && target.closest ? target.closest("select[data-project-select]") : null;
  }
  document.addEventListener("pointerdown", (e) => {
    fillProjectPicker(projectPickerFrom(e.target));
  });
  document.addEventListener("focusin", (e) => {
    fillProjectPicker(projectPickerFrom(e.target));
  });

  /* Shows an overlay as a modal dialog: labels it, puts it on the stack,
     makes the page behind it inert and moves focus into it. */
  function openDialog(el, opener) {
    if (!el || dialogIndex(el) !== -1) return;
    fillApproverPicker(el);
    const sheet = el.querySelector(".sheet") || el;
    sheet.setAttribute("role", "dialog");
    sheet.setAttribute("aria-modal", "true");
    if (!sheet.hasAttribute("aria-label") && !sheet.hasAttribute("aria-labelledby")) {
      const title = sheet.querySelector("h2, h1, h3, .sh-head b");
      if (title) {
        if (!title.id) title.id = `dialog-title-${++dialogTitleCounter}`;
        sheet.setAttribute("aria-labelledby", title.id);
      }
    }
    for (const close of sheet.querySelectorAll(".sh-close:not([aria-label])")) {
      close.setAttribute("aria-label", "Close");
    }
    el.hidden = false;
    const record = { el, opener: opener || null };
    if (opener && opener.hasAttribute("aria-expanded")) opener.setAttribute("aria-expanded", "true");
    openDialogs.push(record);
    syncDialogBackground();

    const list = focusables(el);
    const initial = el.querySelector("[data-dialog-initial-focus]");
    if (initial && list.includes(initial)) {
      initial.focus();
    } else if (list.length) {
      list[0].focus();
    } else {
      el.setAttribute("tabindex", "-1");
      el.focus();
    }
  }

  function closeDialog(el) {
    if (!el) return;
    el.hidden = true;
    const idx = dialogIndex(el);
    if (idx === -1) return;
    const record = openDialogs.splice(idx, 1)[0];
    syncDialogBackground();
    if (record.opener && record.opener.hasAttribute("aria-expanded")) record.opener.setAttribute("aria-expanded", "false");
    /* Focus goes back where the user left it, never to the top of the page. */
    if (record.opener && document.contains(record.opener)) record.opener.focus();
  }

  function topDialog() {
    return openDialogs.length ? openDialogs[openDialogs.length - 1].el : null;
  }

  // The browser's own scroll-on-focus ignores our sticky top bar, tab bar and
  // action bar. Keep a keyboard-focused control in the visible part of the page.
  document.addEventListener("focusin", (event) => {
    const field = event.target;
    if (!field.matches || !field.matches("main input, main select, main textarea, main button, main a")) return;
    if (field.closest(".action-bar, .overlay") || topDialog()) return;
    requestAnimationFrame(() => {
      if (document.activeElement !== field || !field.isConnected) return;
      let top = 0;
      let bottom = window.innerHeight;
      for (const bar of document.querySelectorAll(".m-topbar, .tabbar, .page .action-bar")) {
        const style = getComputedStyle(bar);
        const rect = bar.getBoundingClientRect();
        if (!rect.width || !rect.height || (style.position !== "sticky" && style.position !== "fixed")) continue;
        if (bar.matches(".m-topbar")) top = Math.max(top, rect.bottom);
        else if (rect.top > top && rect.top < bottom) bottom = rect.top;
      }
      const rect = field.getBoundingClientRect();
      if (bottom > top && (rect.top < top + 8 || rect.bottom > bottom - 8)) {
        window.scrollBy(0, (rect.top + rect.bottom) / 2 - (top + bottom) / 2);
      }
    });
  });

  /* Escape closes the top dialog; Tab and Shift+Tab stay inside it. */
  document.addEventListener("keydown", (event) => {
    const el = topDialog();
    if (!el) return;

    if (event.key === "Escape") {
      event.preventDefault();
      closeDialog(el);
      return;
    }
    if (event.key !== "Tab") return;

    const list = focusables(el);
    if (!list.length) {
      event.preventDefault();
      return;
    }
    const first = list[0];
    const last = list[list.length - 1];
    const active = document.activeElement;

    if (!el.contains(active)) {
      event.preventDefault();
      (event.shiftKey ? last : first).focus();
    } else if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  });

  /* ---------------------------------------------------------------- */
  /* data-when="field:value|value" — show a block for chosen values    */
  /* ---------------------------------------------------------------- */

  /* The current value of the named control: "on"/"off" for a checkbox, the
     checked value for radios, otherwise its value. Null if there is none. */
  function sourceValue(scope, name) {
    const all = scope.querySelectorAll(`[name="${name}"]`);
    if (!all.length) return null;

    const first = all[0];
    if (first.type === "checkbox") return first.checked ? "on" : "off";
    if (first.type === "radio") {
      const checked = scope.querySelector(`[name="${name}"]:checked`);
      return checked ? checked.value : "";
    }
    return first.value;
  }

  function syncConditionals(root) {
    for (const node of root.querySelectorAll("[data-when]")) {
      const rule = node.getAttribute("data-when");
      const split = rule.indexOf(":");
      if (split === -1) continue;

      const name = rule.slice(0, split);
      const wanted = rule.slice(split + 1).split("|");
      /* Prefer the enclosing form so two forms on a page cannot cross-wire. */
      const scope = node.closest("form") || document;
      let value = sourceValue(scope, name);
      if (value === null && scope !== document) value = sourceValue(document, name);
      if (value === null) continue;

      node.hidden = !wanted.includes(value);
      // Opt-in only: a field marked data-required-when-visible (the urgency
      // reason) is required exactly while its block is shown. Other fields
      // keep their own rules.
      for (const field of node.querySelectorAll("[data-required-when-visible]")) {
        field.required = !node.hidden;
        field.setAttribute("aria-required", field.required ? "true" : "false");
      }
    }
  }

  /* ---------------------------------------------------------------- */
  /* Money field — Indian digit grouping and the amount in words       */
  /* ---------------------------------------------------------------- */

  function moneyInputs(root) {
    return Array.from(root.querySelectorAll(".money-field input, input[data-money]")).filter((input) => {
      const type = (input.getAttribute("type") || "text").toLowerCase();
      return type === "text" || type === "tel" || type === "search" || type === "number";
    });
  }

  /* Remove display decoration only. Signs, extra decimals and other invalid
     characters must remain available for the user to correct. */
  function rawAmount(value) {
    return String(value == null ? "" : value).trim().replace(/^₹\s*/, "").replace(/,/g, "");
  }

  // Parses a typed amount into integer paise, kept as a decimal string the
  // same way the Go parser works. A floating-point conversion would lose paise
  // before they could be validated. Returns { raw, error, paise, decimal }.
  function moneyValue(value) {
    const raw = rawAmount(value);
    const result = { raw, error: "", paise: "", decimal: "" };
    if (!raw) result.error = "Enter an amount.";
    else if (/^[-−]/.test(raw)) result.error = "Enter a positive amount; negative amounts are not supported.";
    const match = /^\+?(\d*\.?\d*)(?:[eE]([+-]?\d+))?$/.exec(raw);
    if (!result.error && (!match || !/\d/.test(match[1]))) result.error = "Enter a valid amount using digits and up to two decimal places.";
    if (result.error) return result;
    const parts = match[1].split(".");
    const fraction = parts[1] || "";
    const digits = (parts[0] + fraction).replace(/^0+/, "");
    const exponent = Number(match[2] || "0");
    if (!digits) result.error = "Enter an amount greater than zero.";
    else if (exponent < fraction.length - 2) result.error = "Use no more than two decimal places; the amount has not been rounded.";
    else if (exponent > fraction.length + 19 || !Number.isSafeInteger(exponent)) result.error = "This amount is too large.";
    if (result.error) return result;
    const zeros = 2 - (fraction.length - exponent);
    if (digits.length + zeros > 19) {
      result.error = "This amount is too large.";
      return result;
    }
    const paise = digits + "0".repeat(zeros);
    // 9223372036854775807 is the largest int64, the server's limit.
    if (paise.length === 19 && paise > "9223372036854775807") {
      result.error = "This amount is too large.";
      return result;
    }
    result.paise = paise;
    const padded = paise.padStart(3, "0");
    result.decimal = `${padded.slice(0, -2)}.${padded.slice(-2)}`;
    return result;
  }

  /* 12345678.5 -> "1,23,45,678.5": the last three digits, then pairs. */
  function indianGroup(raw) {
    const parts = raw.split(".");
    const whole = parts[0].replace(/^0+(?=\d)/, "");
    const frac = parts.length > 1 ? parts[1] : null;
    let grouped = whole;
    if (whole.length > 3) {
      const last3 = whole.slice(-3);
      grouped = `${whole.slice(0, -3).replace(/\B(?=(\d{2})+(?!\d))/g, ",")},${last3}`;
    }
    return grouped + (frac === null ? "" : `.${frac}`);
  }

  /* A whole number in words using crore and lakh: 150000 -> "One lakh fifty thousand". */
  function inWords(n) {
    n = Math.floor(n);
    if (n === 0) return "Zero";
    const a = ["", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
      "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"];
    const b = ["", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"];
    const two = (x) => (x < 20 ? a[x] : b[Math.floor(x / 10)] + (x % 10 ? ` ${a[x % 10]}` : ""));
    const three = (x) =>
      (x > 99 ? `${a[Math.floor(x / 100)]} hundred${x % 100 ? " " : ""}` : "") + (x % 100 ? two(x % 100) : "");
    const out = [];
    const crore = Math.floor(n / 10000000); n %= 10000000;
    const lakh = Math.floor(n / 100000); n %= 100000;
    const thousand = Math.floor(n / 1000); n %= 1000;
    if (crore) out.push(`${inWords(crore).toLowerCase()} crore`);
    if (lakh) out.push(`${three(lakh)} lakh`);
    if (thousand) out.push(`${three(thousand)} thousand`);
    if (n) out.push(three(n));
    const s = out.join(" ");
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  // Split the displayed decimal string instead of multiplying a floating-point
  // amount: 400.13 must always say thirteen paise, including while typing.
  function amountInWords(raw) {
    const parts = raw.split(".");
    const rupees = Number(parts[0] || "0");
    const paise = Number(((parts[1] || "") + "00").slice(0, 2));
    if (!Number.isSafeInteger(rupees)) return "Amount too large to display in words";
    let words = inWords(rupees) + (rupees === 1 ? " rupee" : " rupees");
    if (paise) words += ` and ${inWords(paise).toLowerCase()}${paise === 1 ? " paisa" : " paise"}`;
    return `${words} only`;
  }

  /* Grouping commas are inserted while typing, so the caret is re-anchored to
     the digit it was on rather than to a raw character offset. */
  function significantBefore(value, caret) {
    let count = 0;
    for (let i = 0; i < caret && i < value.length; i += 1) {
      if (/[\d.]/.test(value.charAt(i))) count += 1;
    }
    return count;
  }

  function offsetAfterSignificant(value, count) {
    if (count <= 0) return 0;
    let seen = 0;
    for (let i = 0; i < value.length; i += 1) {
      if (/[\d.]/.test(value.charAt(i))) {
        seen += 1;
        if (seen === count) return i + 1;
      }
    }
    return value.length;
  }

  function setCaret(input, offset) {
    try {
      input.setSelectionRange(offset, offset);
    } catch (err) {
      /* Some input types refuse selection APIs; formatting still applied. */
    }
  }

  /* Validates the amount, rewrites it with grouping commas and updates the
     amount-in-words line under the field. */
  function formatMoney(input, keepCaret) {
    const field = input.closest(".money-field");
    const out = field ? field.querySelector(".in-words") : null;
    const before = input.value;
    const value = moneyValue(before);
    input.setCustomValidity(value.raw && !input.readOnly && !input.disabled ? value.error : "");

    if (value.error) {
      if (out) {
        out.textContent = value.raw ? value.error : EMPTY_WORDS;
        out.classList.toggle("empty", !value.raw);
      }
      return;
    }

    // Keep exponent notation while typing, and never put grouping commas into
    // number inputs, where the browser would erase the entire value.
    const grouped = /[eE]/.test(value.raw) || input.type === "number"
      ? value.raw : indianGroup(value.raw.replace(/^\+/, ""));
    if (grouped !== before) {
      const caret = keepCaret && input.selectionStart != null
        ? significantBefore(before, input.selectionStart)
        : -1;
      input.value = grouped;
      if (caret !== -1) setCaret(input, offsetAfterSignificant(grouped, caret));
    }
    if (out) {
      out.textContent = amountInWords(value.decimal);
      out.classList.remove("empty");
    }
  }

  function initMoneyFields(root) {
    for (const input of moneyInputs(root)) {
      if (input.getAttribute("data-money-bound") === "1") continue;
      input.setAttribute("data-money-bound", "1");
      input.addEventListener("input", () => { formatMoney(input, true); });
      input.addEventListener("blur", () => { formatMoney(input, false); });
      formatMoney(input, false);
    }
  }

  /* The field shows grouped digits, so the commas are stripped again on the
     way out. Go accepts commas, but plain digits are one less thing for a
     handler to get wrong. Capture phase, so this runs before htmx serialises
     the form. */
  document.addEventListener("submit", (event) => {
    const form = event.target;
    if (!form || typeof form.querySelectorAll !== "function") return;
    const inputs = moneyInputs(form);
    let invalid = false;
    for (const input of inputs) {
      formatMoney(input, false);
      if (input.willValidate && !input.validity.valid) invalid = true;
    }
    if (invalid) {
      event.preventDefault();
      event.stopImmediatePropagation();
      form.reportValidity();
      return;
    }
    for (const input of inputs) {
      if (!moneyValue(input.value).error) input.value = rawAmount(input.value);
    }
  }, true);

  /* ---------------------------------------------------------------- */
  /* Form errors                                                       */
  /* ---------------------------------------------------------------- */

  /* The browser's own validation still decides whether a form may submit;
     this only replaces its small tooltip with an inline message under each
     field and a summary at the top of the form. Without JavaScript the
     browser's normal feedback remains. */
  const validationStates = new WeakMap();
  const validationLabels = new WeakMap();
  let validationID = 0;

  /* The field's human name, taken from its label or legend, without
     "optional"/"required" markers. Cached, because an inline error is later
     inserted into wrapping labels. */
  function validationLabel(field) {
    if (validationLabels.has(field)) return validationLabels.get(field);
    let label = field.getAttribute("aria-label");
    if (!label && field.labels && field.labels.length) {
      const copy = field.labels[0].cloneNode(true);
      for (const node of copy.querySelectorAll("input, select, textarea, .field-error-message, [aria-hidden]")) node.remove();
      label = copy.textContent;
    }
    if (field.type === "radio") {
      const group = field.closest("fieldset");
      const legend = group && group.querySelector("legend");
      label = legend ? legend.textContent : (field.name || "an option").replace(/_/g, " ");
    }
    label = (label || field.name || "This field").replace(/\s+/g, " ").replace(/\s*(?:optional|required)\s*$/i, "").replace(/\s*\*\s*$/, "").trim();
    validationLabels.set(field, label);
    return label;
  }

  /* A plain-language message for whichever constraint the field breaks. */
  function validationMessage(field) {
    const v = field.validity;
    const label = validationLabel(field);
    if (v.valueMissing) {
      if (field.getAttribute("data-validation-required-message")) return field.getAttribute("data-validation-required-message");
      if (label === "What needs correcting") return "Explain what needs correcting.";
      if (field.type === "checkbox") return `Confirm “${label}” to continue.`;
      if (field.type === "radio" || field.tagName === "SELECT") return `Choose ${label}.`;
      if (field.type === "file") return `Choose a file for ${label}.`;
      return `Enter ${label}.`;
    }
    if (v.typeMismatch && field.type === "email") return "Enter a valid email address, such as name@example.com.";
    if (v.typeMismatch && field.type === "url") return "Enter a complete web address, such as https://example.com.";
    if (v.badInput) return `Enter a valid ${field.type === "number" ? "number" : "value"} for ${label}.`;
    if (v.rangeUnderflow) return `Enter ${label}${field.type === "number" || field.type === "range" ? " of at least " : " on or after "}${field.min}.`;
    if (v.rangeOverflow) return `Enter ${label}${field.type === "number" || field.type === "range" ? " of no more than " : " no later than "}${field.max}.`;
    if (v.tooShort) return `Use at least ${field.minLength} characters for ${label}.`;
    if (v.tooLong) return `Use no more than ${field.maxLength} characters for ${label}.`;
    if (v.patternMismatch) return `Enter ${label} in the required format.${field.title ? ` ${field.title}` : ""}`;
    if (v.stepMismatch) return `Enter ${label} in increments of ${field.step || "1"}${field.min ? ` starting at ${field.min}` : ""}.`;
    if (v.customError) return field.validationMessage;
    return `Check ${label} and try again.`;
  }

  /* Per form: the inline message shown for each invalid field, and the summary box. */
  function validationState(form) {
    if (!validationStates.has(form)) validationStates.set(form, { fields: new Map(), summary: null, pending: false });
    return validationStates.get(form);
  }

  function clearValidationField(state, field) {
    const record = state.fields.get(field);
    if (!record) return;
    record.message.remove();
    if (record.moneyWords) record.moneyWords.hidden = false;
    const described = (field.getAttribute("aria-describedby") || "").split(/\s+/).filter((id) => id && id !== record.message.id);
    if (described.length) field.setAttribute("aria-describedby", described.join(" "));
    else field.removeAttribute("aria-describedby");
    if (record.invalid === null) field.removeAttribute("aria-invalid");
    else field.setAttribute("aria-invalid", record.invalid);
    state.fields.delete(field);
  }

  function showValidationField(state, field) {
    let record = state.fields.get(field);
    if (!record) {
      if (!field.id) field.id = `validation-field-${++validationID}`;
      const message = document.createElement("span");
      message.className = "field-error-message client-field-error";
      message.id = `validation-message-${++validationID}`;
      const label = field.closest("label");
      const moneyField = field.closest(".money-field");
      const moneyWords = moneyField && moneyField.querySelector(".in-words");
      // Keep one inline message inside the money field's column. Inserting a
      // sibling of that column creates a narrow extra cell in form grids.
      if (moneyField) moneyField.appendChild(message);
      else (label || field).insertAdjacentElement("afterend", message);
      if (moneyWords) moneyWords.hidden = true;
      record = { message, moneyWords, invalid: field.getAttribute("aria-invalid") };
      state.fields.set(field, record);
      field.setAttribute("aria-describedby", `${field.getAttribute("aria-describedby") || ""} ${message.id}`.trim());
    }
    record.message.textContent = validationMessage(field);
    field.setAttribute("aria-invalid", "true");
  }

  /* Re-checks every reported field, drops fixed ones, and redraws the
     summary: a heading, a sentence, and one link per remaining error. */
  function renderValidationSummary(form, focus) {
    const state = validationState(form);
    state.fields.forEach((record, field) => {
      if (!field.isConnected || !field.willValidate || field.validity.valid) clearValidationField(state, field);
      else record.message.textContent = validationMessage(field);
    });
    if (!state.fields.size) {
      if (state.summary) state.summary.remove();
      state.summary = null;
      return;
    }
    if (!state.summary) {
      state.summary = document.createElement("div");
      state.summary.className = "error-summary client-error-summary";
      state.summary.setAttribute("role", "region");
      state.summary.setAttribute("tabindex", "-1");
      state.summary.setAttribute("aria-label", "Form needs attention");
      const first = state.fields.keys().next().value;
      // Row editors have inputs outside their tiny form in the action cell.
      // Their summary belongs above the table, not inside that narrow cell.
      const table = !form.contains(first) && first.closest("table");
      if (table) table.parentNode.insertBefore(state.summary, table);
      else {
        const dialog = first.closest('[role="dialog"], dialog, .overlay');
        const container = dialog && !dialog.contains(form) ? dialog : form;
        container.insertBefore(state.summary, container.firstChild);
      }
    }
    state.summary.replaceChildren();
    const heading = document.createElement("h2");
    heading.textContent = "Check the highlighted fields";
    const intro = document.createElement("p");
    intro.textContent = "Nothing was submitted. Correct the following to continue.";
    const list = document.createElement("ul");
    const radioNames = new Set();
    state.fields.forEach((record, field) => {
      if (field.type === "radio" && radioNames.has(field.name)) return;
      if (field.type === "radio") radioNames.add(field.name);
      const item = document.createElement("li");
      const link = document.createElement("a");
      link.href = `#${field.id}`;
      link.textContent = record.message.textContent;
      link.addEventListener("click", (event) => {
        event.preventDefault();
        // A required field inside a collapsed accordion remains required.
        // Open only its accordion; do not remove or weaken its constraints.
        const section = field.closest(".acc-item, .pa-item");
        if (section) {
          const head = section.querySelector(HEAD_SELECTOR);
          if (head) setAccordion(head, true);
        }
        field.focus();
        field.scrollIntoView({ block: "center" });
      });
      item.appendChild(link);
      list.appendChild(item);
    });
    state.summary.appendChild(heading);
    state.summary.appendChild(intro);
    state.summary.appendChild(list);
    if (focus) {
      state.summary.focus();
      state.summary.scrollIntoView({ block: "center" });
    }
  }

  /* ---------------------------------------------------------------- */
  /* Payment entry form                                                */
  /* ---------------------------------------------------------------- */

  const REFERENCE_MESSAGE = "Enter a transaction or payment reference; spaces alone are not a reference.";

  /* A reference made only of spaces is no reference. */
  function checkReference(field) {
    field.setCustomValidity(field.value.trim() ? "" : REFERENCE_MESSAGE);
  }

  // The htmx settlement preview must pass the same checks as a submission:
  // the amount parses and is within the remaining approved balance, and the
  // reference is not blank.
  function validatePaymentEntry(form) {
    if (!form || !form.matches("[data-payment-entry]")) return true;
    const amount = form.querySelector('[name="amount"]');
    const reference = form.querySelector("[data-payment-reference]");
    if (amount) {
      const value = moneyValue(amount.value);
      const approved = form.querySelector("[data-approved]");
      let error = value.error || "";
      if (!error && approved && BigInt(value.paise) > BigInt(approved.getAttribute("data-approved"))) {
        error = "Enter an amount no greater than the remaining approved balance.";
      }
      amount.setCustomValidity(error);
    }
    if (reference) checkReference(reference);
    // A dismissed preview keeps its controls until the next htmx swap. Check
    // entry fields only; an unchosen old settlement is not an entry error.
    let valid = true;
    for (const field of form.elements) {
      if (!field.closest("#settle-mount") && typeof field.checkValidity === "function" && !field.checkValidity()) valid = false;
    }
    return valid;
  }

  document.addEventListener("click", (event) => {
    const button = event.target.closest && event.target.closest('button[formaction*="/settlement-preview"]');
    if (button && !validatePaymentEntry(button.form)) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);

  /* ---------------------------------------------------------------- */
  /* Form errors — events                                              */
  /* ---------------------------------------------------------------- */

  /* The browser fires `invalid` once per failing field when a submission is
     blocked. Each gets an inline message; the summary is drawn once, after
     the last of them. */
  document.addEventListener("invalid", (event) => {
    const field = event.target;
    if (!field.form || !field.willValidate) return;
    event.preventDefault();
    const form = field.form;
    // If a stale or scripted confirmation hits an entry error, close the
    // payment confirmation sheet so the error is visible.
    const sheet = form.querySelector("#settle-sheet");
    if (sheet && !sheet.contains(field)) closeDialog(sheet);
    const state = validationState(form);
    // Save the name before an inline error is inserted into a wrapping label.
    validationLabel(field);
    if (field.type === "radio" && Array.from(state.fields.keys()).some((other) => other.type === "radio" && other.name === field.name)) return;
    showValidationField(state, field);
    if (!state.pending) {
      state.pending = true;
      setTimeout(() => {
        state.pending = false;
        if (form.isConnected) renderValidationSummary(form, true);
      }, 0);
    }
  }, true);

  /* While the user corrects a form, clear the errors they have fixed. */
  function updateValidation(event) {
    const field = event.target;
    if (!field.form) return;
    const form = field.form;
    if (field.matches("[data-payment-reference]")) checkReference(field);
    // Run after conditional handlers have disabled irrelevant controls. Only
    // revisit reported errors; untouched fields stay quiet while typing.
    setTimeout(() => {
      if (!form.isConnected) return;
      if (validationStates.has(form) && validationState(form).fields.size) renderValidationSummary(form, false);
      // The server's "give a reason for urgency" banner goes once the reason
      // is no longer needed or has been written.
      const urgency = form.querySelector('[name="urgency_reason"][data-required-when-visible]');
      if (urgency && (!urgency.required || urgency.value.trim())) {
        for (const banner of document.querySelectorAll('[data-server-error-for="urgency_reason"]')) banner.remove();
      }
    }, 0);
  }
  document.addEventListener("input", updateValidation);
  document.addEventListener("change", updateValidation);
  document.addEventListener("reset", (event) => {
    const state = validationStates.get(event.target);
    if (!state) return;
    state.fields.forEach((record, field) => { clearValidationField(state, field); });
    if (state.summary) state.summary.remove();
    state.summary = null;
  });

  /* ---------------------------------------------------------------- */
  /* Clicks, handled once for the whole page                           */
  /* ---------------------------------------------------------------- */

  document.addEventListener("click", (event) => {
    const target = event.target;
    if (!target || typeof target.closest !== "function") return;

    /* Mobile "More" tab */
    const moreTab = target.closest(".js-more");
    if (moreTab) {
      event.preventDefault();
      openDialog(document.querySelector(".more-sheet"), moreTab);
      return;
    }
    const moreClose = target.closest(".ms-close");
    if (moreClose) {
      event.preventDefault();
      closeDialog(moreClose.closest(".more-sheet") || document.querySelector(".more-sheet"));
      return;
    }

    /* Accordions */
    const head = target.closest(HEAD_SELECTOR);
    if (head) {
      event.preventDefault();
      toggleAccordion(head);
      return;
    }

    /* Combobox pick.

       Choosing a result writes its id into the hidden [data-combo-value] the
       form actually posts, and its name into the visible box. The hidden id is
       what the server validates; the visible text is never trusted. Where the
       field has no hidden value to fill — a picker used purely for navigation
       — the row stays the ordinary link it already is, which is also what a
       browser with no scripting gets. */
    const option = target.closest(".combo-list [data-id]");
    if (option) {
      const field = option.closest(".field");
      const hidden = field && field.querySelector("[data-combo-value]");
      if (hidden) {
        event.preventDefault();
        hidden.value = option.getAttribute("data-id");
        const visible = field.querySelector(".combo-input");
        if (visible) visible.value = option.getAttribute("data-name") || "";
        const list = option.closest(".combo-list");
        if (list && list.parentNode) list.parentNode.innerHTML = "";
        return;
      }
    }

    /* Buttons that open or close a dialog by id: data-open="id", data-close="id" */
    const opener = target.closest("[data-open]");
    if (opener) {
      event.preventDefault();
      openDialog(document.getElementById(opener.getAttribute("data-open")), opener);
      return;
    }
    const closer = target.closest("[data-close]");
    if (closer) {
      event.preventDefault();
      closeDialog(document.getElementById(closer.getAttribute("data-close")));
      return;
    }

    /* A click on the dimmed backdrop itself (never inside the sheet) closes it. */
    if (target.classList && target.classList.contains("overlay")) {
      closeDialog(target);
      return;
    }

    /* Variance grid */
    const projectToggle = target.closest("[data-project-toggle]");
    if (projectToggle) {
      toggleProject(projectToggle);
    }
  });

  document.addEventListener("change", () => {
    syncConditionals(document);
  });

  /* ---------------------------------------------------------------- */
  /* Roles matrix — a desktop table and a phone accordion, one form    */
  /* ---------------------------------------------------------------- */

  /* Both renderings live in one <form>, so exactly one may submit: the other
     one's inputs are disabled. Without JavaScript the desktop table is used
     (it scrolls inside .table-wrap at any width); below 860px the accordion
     takes over. Nothing here decides what may be granted: the server checks
     every submitted cell and permission against its own list. */
  function initRoleMatrix(root) {
    const form = root.querySelector ? root.querySelector("#role-form") : null;
    if (!form || form.getAttribute("data-matrix-bound") === "1") return;
    const desktop = form.querySelector(".perm-table");
    const mobile = form.querySelector(".perm-acc");
    if (!desktop || !mobile) return;
    form.setAttribute("data-matrix-bound", "1");

    const narrow = window.matchMedia("(max-width: 860px)");
    const setDisabled = (scope, off) => {
      for (const input of scope.querySelectorAll("input")) input.disabled = off;
    };
    const handOver = () => {
      setDisabled(desktop, narrow.matches);
      setDisabled(mobile, !narrow.matches);
    };
    narrow.addEventListener("change", handOver);
    handOver();

    /* A cell stands for every permission behind it: ticking or clearing it
       ticks or clears all of its Advanced checkboxes, so clearing a cell
       really does revoke. It also works the other way: when only some boxes
       under a cell are ticked, the cell shows as indeterminate and unchecked,
       so the cell is not submitted and the boxes are what the server saves
       (the server draws that partial state on load as data-partial). Both
       renderings are kept equal so resizing mid-edit loses nothing. */
    const all = (selector) => Array.from(form.querySelectorAll(selector));
    const syncCell = (value) => {
      const boxes = all(`input[name="perm"][data-cell="${window.CSS.escape(value)}"]`);
      const ticked = boxes.filter((box) => box.checked).length;
      for (const cell of all(`input[name="cell"][value="${window.CSS.escape(value)}"]`)) {
        cell.checked = boxes.length > 0 && ticked === boxes.length;
        cell.indeterminate = ticked > 0 && !cell.checked;
      }
    };
    /* The accordion badge counts the row's held grants out of what it maps. */
    const syncBadges = () => {
      for (const item of mobile.querySelectorAll(".pa-item")) {
        const badge = item.querySelector(".pa-head .n");
        const boxes = item.querySelectorAll('input[name="perm"]');
        const ticked = item.querySelectorAll('input[name="perm"]:checked').length;
        if (badge) badge.textContent = `${ticked} of ${boxes.length}`;
      }
    };
    for (const cell of all('input[name="cell"][data-partial]')) cell.indeterminate = true;
    form.addEventListener("change", (event) => {
      const input = event.target;
      if (!input || !input.matches) return;
      if (input.matches('input[name="cell"]')) {
        for (const box of all(`input[name="perm"][data-cell="${window.CSS.escape(input.value)}"]`)) box.checked = input.checked;
        syncCell(input.value);
        syncBadges();
      } else if (input.matches('input[name="perm"]')) {
        for (const box of all(`input[name="perm"][value="${window.CSS.escape(input.value)}"]`)) box.checked = input.checked;
        if (input.getAttribute("data-cell")) syncCell(input.getAttribute("data-cell"));
        syncBadges();
      } else if (input.matches('.perm-scope input[type="radio"]')) {
        /* The server lights the saved scope's pill with .is-on; from here on
           the highlight follows the selected radio. */
        const group = input.closest(".perm-scope");
        if (!group) return;
        for (const label of group.querySelectorAll("label")) {
          const radio = label.querySelector("input");
          label.classList.toggle("is-on", !!radio && radio.checked);
        }
      }
    });
  }

  /* ---------------------------------------------------------------- */
  /* Payment amount compared with the approved amount                  */
  /* ---------------------------------------------------------------- */

  /* Display only: shows how the typed amount compares with the approved
     amount while the accountant types. The server refuses payments above the
     approved amount regardless. Uses the money field's own grouping so the
     page has one number format. */
  function initDifferenceBanner(root) {
    for (const source of root.querySelectorAll("[data-approved]")) {
      if (source.getAttribute("data-diff-bound") === "1") continue;
      const approved = BigInt(source.getAttribute("data-approved") || "0");
      const paid = document.getElementById("amount");
      const banner = document.getElementById("diff-banner");
      const text = document.getElementById("diff-text");
      if (!paid || !banner || !text) continue;
      source.setAttribute("data-diff-bound", "1");

      const sync = () => {
        const value = moneyValue(paid.value);
        if (value.error) {
          banner.hidden = true;
          return;
        }
        banner.hidden = false;
        const diff = approved - BigInt(value.paise);
        banner.className = `banner ${diff === 0n ? "good" : diff > 0n ? "warn" : "bad"}`;
        if (diff === 0n) {
          text.textContent = "Matches the approved amount exactly";
        } else {
          const absolute = diff < 0n ? -diff : diff;
          const rupees = indianGroup(`${(absolute / 100n).toString()}.${(absolute % 100n).toString().padStart(2, "0")}`);
          text.textContent = diff > 0n
            ? `₹${rupees} less than approved`
            : `₹${rupees} more than approved — not allowed`;
        }
      };

      paid.addEventListener("input", sync);
      paid.addEventListener("blur", sync);
      sync();
    }
  }

  /* ---------------------------------------------------------------- */
  /* data-follows-select — a label that names the chosen option        */
  /* ---------------------------------------------------------------- */

  /* An element with data-follows-select="<select id>" and
     data-follows-text="… {name} …" rewrites its text from the chosen option
     whenever the select changes, so a button reading "Save and notify Mona"
     does not stay that way after the select is changed to Max. The server
     renders the right text for the saved value, so the page reads correctly
     without this. */
  function initFollowSelects(root) {
    for (const label of root.querySelectorAll("[data-follows-select]")) {
      if (label.dataset.followsBound) continue;
      const select = document.getElementById(label.getAttribute("data-follows-select"));
      const text = label.getAttribute("data-follows-text");
      if (!select || !text) continue;
      label.dataset.followsBound = "1";

      const sync = () => {
        const option = select.options[select.selectedIndex];
        if (!option) return;
        label.textContent = text.replace("{name}", option.textContent.trim());
      };

      select.addEventListener("change", sync);
      sync();
    }
  }

  /* ---------------------------------------------------------------- */
  /* Start-up, and again after every htmx swap                         */
  /* ---------------------------------------------------------------- */

  /* A sheet the server renders already open (data-open-on-load, not hidden)
     — typically a refused form sent back with the user's input and an inline
     error — joins the dialog stack as if the user had opened it, so Escape,
     the focus trap and Cancel all work. */
  function openServedDialogs(root) {
    for (const el of root.querySelectorAll(".overlay[data-open-on-load]:not([hidden])")) {
      const opener = Array.from(root.querySelectorAll("[data-open]")).find(
        (button) => button.getAttribute("data-open") === el.id && button.getClientRects().length > 0
      );
      openDialog(el, opener || null);
    }
  }

  /* A sheet the server renders hidden with data-reopen (a refused form) is
     opened on load. The attribute is removed on first use, so a later htmx
     swap never reopens a sheet the user has closed. */
  function reopenRefusedSheets(root) {
    const sheets = root.querySelectorAll ? root.querySelectorAll(".overlay[data-reopen]") : [];
    for (const el of sheets) {
      el.removeAttribute("data-reopen");
      openDialog(el, null);
    }
  }

  /* Moves focus to a server-rendered error summary once, so a screen reader
     announces it. Summaries drawn by this file focus themselves. */
  function focusErrorSummary(root) {
    const summary = root.querySelector(".error-summary:not(.client-error-summary):not([data-focused])");
    if (!summary || !summary.getClientRects().length) return;
    summary.setAttribute("data-focused", "true");
    summary.setAttribute("tabindex", "-1");
    summary.focus();
  }

  function redrawValidationSummaries() {
    for (const form of document.forms) {
      if (validationStates.has(form)) renderValidationSummary(form, false);
    }
  }

  /* Wires up the whole document. It runs on page load and again after each
     htmx swap, always over the whole document, so every step must be safe to
     repeat: each one skips elements it has already handled. */
  function init() {
    initAccordions(document);
    syncConditionals(document);
    redrawValidationSummaries();
    initMoneyFields(document);
    initDifferenceBanner(document);
    initRoleMatrix(document);
    initFollowSelects(document);
    reopenRefusedSheets(document);
    openServedDialogs(document);
    focusErrorSummary(document);
  }

  /* A filter form that updates the URL (the grid's) can send the same filters
     twice for one gesture: Enter in the search box commits a change and then
     submits, and the live search fires again when its pause runs out. Each
     extra request would only swap in the same rows and push a duplicate
     history entry, so a request repeating the last filters sent is dropped,
     and so is a live one for the filters the page already shows. Back and
     Forward reset this, because the page then shows whatever the server
     rendered for that URL. */
  let lastFilters = new WeakMap();
  document.addEventListener("htmx:configRequest", (event) => {
    const form = event.detail && event.detail.elt;
    if (!form || !form.matches || !form.matches("form[data-skip-shown-filters]")) return;
    const trigger = event.detail.triggeringEvent;
    const wanted = new URLSearchParams(new FormData(form)).toString();
    const shown = form.getAttribute("action") === window.location.pathname &&
      new URLSearchParams(window.location.search).toString() === wanted;
    if (lastFilters.get(form) === wanted || (shown && !(trigger && trigger.type === "submit"))) {
      event.preventDefault();
      return;
    }
    lastFilters.set(form, wanted);
  });
  document.addEventListener("htmx:historyRestore", () => { lastFilters = new WeakMap(); });

  document.addEventListener("htmx:afterSettle", redrawValidationSummaries);
  document.addEventListener("htmx:afterSwap", () => { init(); });
  document.addEventListener("htmx:load", () => { init(); });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  /* ---------------------------------------------------------------- */
  /* data-confirm and data-auto-submit                                 */
  /* ---------------------------------------------------------------- */

  /* A form with data-confirm="question" asks before submitting (the page's
     Content Security Policy forbids inline onsubmit handlers). */
  document.addEventListener("submit", (event) => {
    const message = event.target.dataset.confirm;
    if (message && !window.confirm(message)) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);

  /* A control with data-auto-submit submits its form as soon as it changes. */
  document.addEventListener("change", (event) => {
    if (event.target.hasAttribute("data-auto-submit") && event.target.form) event.target.form.requestSubmit();
  });
})();
