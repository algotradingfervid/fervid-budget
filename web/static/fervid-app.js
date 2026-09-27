/* ==========================================================================
   Fervid Budget — design system behaviours
   Accordions, overlays/sheets, `[data-when]` conditional reveal, the money
   field, the mobile More sheet, and the variance-grid project toggle.

   Everything here is progressive enhancement over server-rendered markup.
   Nothing decides what a user is allowed to see: visibility of permitted
   actions is settled server-side in the template. `hidden` here is chrome,
   never a security boundary — every conditional field is re-validated in Go.
   ========================================================================== */

(function () {
  "use strict";

  var EMPTY_WORDS = "Enter the amount you are requesting";

  /* ---------------------------------------------------------------- */
  /* Variance grid — project row collapse                              */
  /* ---------------------------------------------------------------- */

  function toggleProject(button) {
    var projectID = button.getAttribute("data-project-toggle");
    var table = button.closest("table");
    if (!projectID || !table) return;

    var isExpanded = button.getAttribute("aria-expanded") !== "false";
    var rows = table.querySelectorAll('[data-project-row="' + projectID + '"]');
    for (var i = 0; i < rows.length; i += 1) {
      rows[i].hidden = isExpanded;
    }

    button.setAttribute("aria-expanded", isExpanded ? "false" : "true");
    var projectRow = button.closest(".project-row");
    if (projectRow) {
      projectRow.classList.toggle("is-collapsed", isExpanded);
    }
  }

  /* ---------------------------------------------------------------- */
  /* Accordions — .acc-head/.acc-item and .pa-head/.pa-item            */
  /* ---------------------------------------------------------------- */

  var HEAD_SELECTOR = ".acc-head, .pa-head";
  var uid = 0;

  function accParts(head) {
    var item = head.closest(".acc-item, .pa-item");
    if (!item) return null;
    var body = item.querySelector(".acc-body, .pa-body");
    if (!body) return null;
    return { item: item, body: body };
  }

  function setAccordion(head, open) {
    var parts = accParts(head);
    if (!parts) return;
    parts.item.classList.toggle("is-open", open);
    parts.body.hidden = !open;
    head.setAttribute("aria-expanded", open ? "true" : "false");
    if (!parts.body.id) {
      uid += 1;
      parts.body.id = "acc-body-" + uid;
    }
    head.setAttribute("aria-controls", parts.body.id);
  }

  /* The open/closed truth ships in the markup as `is-open` on the item;
     this republishes it through the button and the body so assistive tech
     and CSS agree. `.acc-head` should be a real <button type="button">;
     anything else is upgraded below so it is at least operable. */
  function initAccordions(root) {
    Array.prototype.forEach.call(root.querySelectorAll(HEAD_SELECTOR), function (head) {
      var parts = accParts(head);
      if (!parts) return;
      if (head.tagName !== "BUTTON") {
        head.setAttribute("role", "button");
        if (!head.hasAttribute("tabindex")) head.setAttribute("tabindex", "0");
      }
      setAccordion(head, parts.item.classList.contains("is-open"));
    });
  }

  function toggleAccordion(head) {
    setAccordion(head, head.getAttribute("aria-expanded") === "false");
  }

  /* ---------------------------------------------------------------- */
  /* Overlays, sheets and the More sheet                               */
  /* ---------------------------------------------------------------- */

  var FOCUSABLE = [
    "a[href]",
    "button:not([disabled])",
    "input:not([disabled]):not([type='hidden'])",
    "select:not([disabled])",
    "textarea:not([disabled])",
    "[tabindex]:not([tabindex='-1'])"
  ].join(",");

  /* Stack so a sheet opened from a sheet closes in the right order. */
  var openDialogs = [];
  var dialogBackground = [];
  var dialogBodyOverflow = null;
  var dialogTitleCounter = 0;

  function syncDialogBackground() {
    dialogBackground.forEach(function (node) { node.inert = false; });
    dialogBackground = [];
    var dialog = topDialog();
    if (!dialog) {
      if (dialogBodyOverflow !== null) document.body.style.overflow = dialogBodyOverflow;
      dialogBodyOverflow = null;
      return;
    }
    if (dialogBodyOverflow === null) dialogBodyOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    // Recompute from the top sheet so nested dialogs do not leave their own
    // ancestors inert. Preserve any inert state not imposed by this helper.
    for (var branch = dialog; branch.parentElement; branch = branch.parentElement) {
      Array.prototype.forEach.call(branch.parentElement.children, function (sibling) {
        if (sibling !== branch && !sibling.inert) {
          sibling.inert = true;
          dialogBackground.push(sibling);
        }
      });
      if (branch.parentElement === document.body) break;
    }
  }

  function focusables(root) {
    return Array.prototype.filter.call(root.querySelectorAll(FOCUSABLE), function (node) {
      return node.getClientRects().length > 0;
    });
  }

  function dialogIndex(el) {
    for (var i = openDialogs.length - 1; i >= 0; i -= 1) {
      if (openDialogs[i].el === el) return i;
    }
    return -1;
  }

  /**
   * Fills an approver picker from the one shared option list on the page.
   *
   * The people screen used to render every approver inside every user's panel,
   * which is quadratic: 56 people meant ~3,000 option elements and 346 KB of
   * markup before anything was opened. The options are now emitted once, and the
   * panel being opened borrows them. Self-approval is not allowed, so the person
   * the panel belongs to is left out of their own list.
   */
  function fillApproverPicker(root) {
    var source = document.getElementById("approver-options");
    if (!source) return;
    var selects = root.querySelectorAll("select[data-approver-select]");
    for (var i = 0; i < selects.length; i++) {
      var sel = selects[i];
      if (sel.dataset.filled === "1") continue;
      var self = sel.getAttribute("data-self");
      var chosen = sel.getAttribute("data-chosen") || "0";
      var options = source.content.cloneNode(true).querySelectorAll("option");
      for (var j = 0; j < options.length; j++) {
        var opt = options[j];
        if (opt.value === self) continue;
        if (opt.value === chosen) opt.selected = true;
        sel.appendChild(opt);
      }
      sel.dataset.filled = "1";
    }
  }

  /**
   * Fills a project picker from the one shared option list on the page.
   *
   * The heads screen carries one picker per row and used to render every project
   * inside every one of them: 229 heads against 24 projects is about 5,500
   * option elements and 470 KB, of which a reader looks at one. Each row now
   * ships only the project it is filed under, and borrows the rest the moment
   * somebody reaches for the control.
   */
  function fillProjectPicker(sel) {
    if (!sel || sel.dataset.filled === "1") return;
    var source = document.getElementById("project-options");
    if (!source) return;
    var chosen = sel.getAttribute("data-chosen");
    sel.innerHTML = "";
    var options = source.content.cloneNode(true).querySelectorAll("option");
    for (var i = 0; i < options.length; i++) {
      if (options[i].value === chosen) options[i].selected = true;
      sel.appendChild(options[i]);
    }
    sel.dataset.filled = "1";
  }

  // pointerdown as well as focusin: a mouse click opens the native dropdown, and
  // pointerdown is the last moment before it does.
  function projectPickerFrom(target) {
    return target && target.closest ? target.closest("select[data-project-select]") : null;
  }
  document.addEventListener("pointerdown", function (e) {
    fillProjectPicker(projectPickerFrom(e.target));
  });
  document.addEventListener("focusin", function (e) {
    fillProjectPicker(projectPickerFrom(e.target));
  });

  function openDialog(el, opener) {
    if (!el || dialogIndex(el) !== -1) return;
    fillApproverPicker(el);
    var sheet = el.querySelector(".sheet") || el;
    sheet.setAttribute("role", "dialog");
    sheet.setAttribute("aria-modal", "true");
    if (!sheet.hasAttribute("aria-label") && !sheet.hasAttribute("aria-labelledby")) {
      var title = sheet.querySelector("h2, h1, h3, .sh-head b");
      if (title) {
        if (!title.id) title.id = "dialog-title-" + (++dialogTitleCounter);
        sheet.setAttribute("aria-labelledby", title.id);
      }
    }
    Array.prototype.forEach.call(sheet.querySelectorAll(".sh-close:not([aria-label])"), function (close) {
      close.setAttribute("aria-label", "Close");
    });
    el.hidden = false;
    var record = { el: el, opener: opener || null };
    if (opener && opener.hasAttribute("aria-expanded")) opener.setAttribute("aria-expanded", "true");
    openDialogs.push(record);
    syncDialogBackground();

    var list = focusables(el);
    var initial = el.querySelector("[data-dialog-initial-focus]");
    if (initial && list.indexOf(initial) !== -1) {
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
    var idx = dialogIndex(el);
    if (idx === -1) return;
    var record = openDialogs.splice(idx, 1)[0];
    syncDialogBackground();
    if (record.opener && record.opener.hasAttribute("aria-expanded")) record.opener.setAttribute("aria-expanded", "false");
    /* Focus goes back where the user left it, never to the top of the page. */
    if (record.opener && document.contains(record.opener)) record.opener.focus();
  }

  function topDialog() {
    return openDialogs.length ? openDialogs[openDialogs.length - 1].el : null;
  }

  // Native focus scrolling does not account for our sticky action/navigation
  // bars. Keep keyboard-focused fields in the unobscured part of the page.
  document.addEventListener("focusin", function (event) {
    var field = event.target;
    if (!field.matches || !field.matches("main input, main select, main textarea, main button, main a")) return;
    if (field.closest(".action-bar, .overlay") || topDialog()) return;
    requestAnimationFrame(function () {
      if (document.activeElement !== field || !field.isConnected) return;
      var top = 0;
      var bottom = window.innerHeight;
      Array.prototype.forEach.call(document.querySelectorAll(".m-topbar, .tabbar, .page .action-bar"), function (bar) {
        var style = getComputedStyle(bar);
        var rect = bar.getBoundingClientRect();
        if (!rect.width || !rect.height || (style.position !== "sticky" && style.position !== "fixed")) return;
        if (bar.matches(".m-topbar")) top = Math.max(top, rect.bottom);
        else if (rect.top > top && rect.top < bottom) bottom = rect.top;
      });
      var rect = field.getBoundingClientRect();
      if (bottom > top && (rect.top < top + 8 || rect.bottom > bottom - 8)) {
        window.scrollBy(0, (rect.top + rect.bottom) / 2 - (top + bottom) / 2);
      }
    });
  });

  /* Escape closes; Tab cycles within the sheet instead of escaping behind it.
     The mockup had neither — a modal you can tab out of is not a modal. */
  document.addEventListener("keydown", function (event) {
    var el = topDialog();
    if (!el) return;

    if (event.key === "Escape" || event.key === "Esc") {
      event.preventDefault();
      closeDialog(el);
      return;
    }
    if (event.key !== "Tab") return;

    var list = focusables(el);
    if (!list.length) {
      event.preventDefault();
      return;
    }
    var first = list[0];
    var last = list[list.length - 1];
    var active = document.activeElement;

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
  /* [data-when="field:value|value"] conditional reveal                */
  /* ---------------------------------------------------------------- */

  function sourceValue(scope, name) {
    var all = scope.querySelectorAll('[name="' + name + '"]');
    if (!all.length) return null;

    var first = all[0];
    if (first.type === "checkbox") return first.checked ? "on" : "off";
    if (first.type === "radio") {
      var checked = scope.querySelector('[name="' + name + '"]:checked');
      return checked ? checked.value : "";
    }
    return first.value;
  }

  function syncConditionals(root) {
    Array.prototype.forEach.call(root.querySelectorAll("[data-when]"), function (node) {
      var rule = node.getAttribute("data-when");
      var split = rule.indexOf(":");
      if (split === -1) return;

      var name = rule.slice(0, split);
      var wanted = rule.slice(split + 1).split("|");
      /* Prefer the enclosing form so two forms on a page cannot cross-wire. */
      var scope = node.closest("form") || document;
      var value = sourceValue(scope, name);
      if (value === null && scope !== document) value = sourceValue(document, name);
      if (value === null) return;

      node.hidden = wanted.indexOf(value) === -1;
      // Opt-in only: urgency reason is required when its own branch is shown.
      // Other aria-required fields keep their existing custom/server rules.
      Array.prototype.forEach.call(node.querySelectorAll("[data-required-when-visible]"), function (field) {
        field.required = !node.hidden;
        field.setAttribute("aria-required", field.required ? "true" : "false");
      });
    });
  }

  /* ---------------------------------------------------------------- */
  /* Money field — Indian grouping and the amount in words             */
  /* ---------------------------------------------------------------- */

  function moneyInputs(root) {
    return Array.prototype.filter.call(root.querySelectorAll(".money-field input, input[data-money]"), function (input) {
      var type = (input.getAttribute("type") || "text").toLowerCase();
      return type === "text" || type === "tel" || type === "search" || type === "number";
    });
  }

  /* Remove display decoration only. Signs, extra decimals and other invalid
     characters must remain available for the user to correct. */
  function rawAmount(value) {
    return String(value == null ? "" : value).trim().replace(/^₹\s*/, "").replace(/,/g, "");
  }

  // Use decimal strings throughout, matching Go's integer-paise parser. A
  // floating-point conversion would lose paise before we could validate them.
  function moneyValue(value) {
    var raw = rawAmount(value);
    var result = { raw: raw, error: "", paise: "", decimal: "" };
    if (!raw) result.error = "Enter an amount.";
    else if (/^[-−]/.test(raw)) result.error = "Enter a positive amount; negative amounts are not supported.";
    var match = /^\+?(\d*\.?\d*)(?:[eE]([+-]?\d+))?$/.exec(raw);
    if (!result.error && (!match || !/\d/.test(match[1]))) result.error = "Enter a valid amount using digits and up to two decimal places.";
    if (result.error) return result;
    var parts = match[1].split(".");
    var fraction = parts[1] || "";
    var digits = (parts[0] + fraction).replace(/^0+/, "");
    var exponent = Number(match[2] || "0");
    if (!digits) result.error = "Enter an amount greater than zero.";
    else if (exponent < fraction.length - 2) result.error = "Use no more than two decimal places; the amount has not been rounded.";
    else if (exponent > fraction.length + 19 || !Number.isSafeInteger(exponent)) result.error = "This amount is too large.";
    if (result.error) return result;
    var zeros = 2 - (fraction.length - exponent);
    if (digits.length + zeros > 19) {
      result.error = "This amount is too large.";
      return result;
    }
    var paise = digits + "0".repeat(zeros);
    if (paise.length === 19 && paise > "9223372036854775807") {
      result.error = "This amount is too large.";
      return result;
    }
    result.paise = paise;
    var padded = paise.padStart(3, "0");
    result.decimal = padded.slice(0, -2) + "." + padded.slice(-2);
    return result;
  }

  function indianGroup(raw) {
    var parts = raw.split(".");
    var whole = parts[0].replace(/^0+(?=\d)/, "");
    var frac = parts.length > 1 ? parts[1] : null;
    var grouped = whole;
    if (whole.length > 3) {
      var last3 = whole.slice(-3);
      grouped = whole.slice(0, -3).replace(/\B(?=(\d{2})+(?!\d))/g, ",") + "," + last3;
    }
    return grouped + (frac === null ? "" : "." + frac);
  }

  function inWords(n) {
    n = Math.floor(n);
    if (n === 0) return "Zero";
    var a = ["", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
      "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"];
    var b = ["", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"];
    function two(x) { return x < 20 ? a[x] : b[Math.floor(x / 10)] + (x % 10 ? " " + a[x % 10] : ""); }
    function three(x) {
      return (x > 99 ? a[Math.floor(x / 100)] + " hundred" + (x % 100 ? " " : "") : "") + (x % 100 ? two(x % 100) : "");
    }
    var out = [];
    var crore = Math.floor(n / 10000000); n %= 10000000;
    var lakh = Math.floor(n / 100000); n %= 100000;
    var thousand = Math.floor(n / 1000); n %= 1000;
    if (crore) out.push(inWords(crore).toLowerCase() + " crore");
    if (lakh) out.push(three(lakh) + " lakh");
    if (thousand) out.push(three(thousand) + " thousand");
    if (n) out.push(three(n));
    var s = out.join(" ");
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  // Split the displayed decimal string instead of multiplying a floating-point
  // amount: 400.13 must always say thirteen paise, including while typing.
  function amountInWords(raw) {
    var parts = raw.split(".");
    var rupees = Number(parts[0] || "0");
    var paise = Number(((parts[1] || "") + "00").slice(0, 2));
    if (!Number.isSafeInteger(rupees)) return "Amount too large to display in words";
    var words = inWords(rupees) + (rupees === 1 ? " rupee" : " rupees");
    if (paise) words += " and " + inWords(paise).toLowerCase() + (paise === 1 ? " paisa" : " paise");
    return words + " only";
  }

  /* Grouping is inserted while typing, so the caret has to be re-anchored to
     the character it was on rather than to a raw offset. */
  function significantBefore(value, caret) {
    var count = 0;
    for (var i = 0; i < caret && i < value.length; i += 1) {
      if (/[\d.]/.test(value.charAt(i))) count += 1;
    }
    return count;
  }

  function offsetAfterSignificant(value, count) {
    if (count <= 0) return 0;
    var seen = 0;
    for (var i = 0; i < value.length; i += 1) {
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

  function formatMoney(input, keepCaret) {
    var field = input.closest(".money-field");
    var out = field ? field.querySelector(".in-words") : null;
    var before = input.value;
    var value = moneyValue(before);
    input.setCustomValidity(value.raw && !input.readOnly && !input.disabled ? value.error : "");

    if (value.error) {
      if (out) {
        out.textContent = value.raw ? value.error : EMPTY_WORDS;
        out.classList.toggle("empty", !value.raw);
      }
      return;
    }

    // Preserve exponent notation while typing and never put grouping commas
    // into number inputs, where the browser would erase the entire value.
    var grouped = /[eE]/.test(value.raw) || input.type === "number"
      ? value.raw : indianGroup(value.raw.replace(/^\+/, ""));
    if (grouped !== before) {
      var caret = keepCaret && input.selectionStart != null
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
    moneyInputs(root).forEach(function (input) {
      if (input.getAttribute("data-money-bound") === "1") return;
      input.setAttribute("data-money-bound", "1");
      input.addEventListener("input", function () { formatMoney(input, true); });
      input.addEventListener("blur", function () { formatMoney(input, false); });
      formatMoney(input, false);
    });
  }

  /* The field writes grouped digits back into the input, so the grouping is
     stripped again on the way out. Go accepts commas, but plain digits are
     one less thing for a handler to get wrong. Capture phase, so this runs
     before htmx serialises the form. */
  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!form || typeof form.querySelectorAll !== "function") return;
    var inputs = moneyInputs(form);
    var invalid = false;
    inputs.forEach(function (input) {
      formatMoney(input, false);
      if (input.willValidate && !input.validity.valid) invalid = true;
    });
    if (invalid) {
      event.preventDefault();
      event.stopImmediatePropagation();
      form.reportValidity();
      return;
    }
    inputs.forEach(function (input) {
      if (!moneyValue(input.value).error) input.value = rawAmount(input.value);
    });
  }, true);

  /* App-owned presentation of the browser's constraint validation. The native
     validation gate stays enabled; preventing `invalid` only suppresses its
     small tooltip. Without JavaScript the browser's normal feedback remains. */
  var validationStates = new WeakMap();
  var validationLabels = new WeakMap();
  var validationID = 0;

  function validationLabel(field) {
    if (validationLabels.has(field)) return validationLabels.get(field);
    var label = field.getAttribute("aria-label");
    if (!label && field.labels && field.labels.length) {
      var copy = field.labels[0].cloneNode(true);
      Array.prototype.forEach.call(copy.querySelectorAll("input, select, textarea, .field-error-message, [aria-hidden]"), function (node) { node.remove(); });
      label = copy.textContent;
    }
    if (field.type === "radio") {
      var group = field.closest("fieldset");
      var legend = group && group.querySelector("legend");
      label = legend ? legend.textContent : (field.name || "an option").replace(/_/g, " ");
    }
    label = (label || field.name || "This field").replace(/\s+/g, " ").replace(/\s*(?:optional|required)\s*$/i, "").replace(/\s*\*\s*$/, "").trim();
    validationLabels.set(field, label);
    return label;
  }

  function validationMessage(field) {
    var v = field.validity;
    var label = validationLabel(field);
    if (v.valueMissing) {
      if (field.getAttribute("data-validation-required-message")) return field.getAttribute("data-validation-required-message");
      if (label === "What needs correcting") return "Explain what needs correcting.";
      if (field.type === "checkbox") return "Confirm “" + label + "” to continue.";
      if (field.type === "radio" || field.tagName === "SELECT") return "Choose " + label + ".";
      if (field.type === "file") return "Choose a file for " + label + ".";
      return "Enter " + label + ".";
    }
    if (v.typeMismatch && field.type === "email") return "Enter a valid email address, such as name@example.com.";
    if (v.typeMismatch && field.type === "url") return "Enter a complete web address, such as https://example.com.";
    if (v.badInput) return "Enter a valid " + (field.type === "number" ? "number" : "value") + " for " + label + ".";
    if (v.rangeUnderflow) return "Enter " + label + (field.type === "number" || field.type === "range" ? " of at least " : " on or after ") + field.min + ".";
    if (v.rangeOverflow) return "Enter " + label + (field.type === "number" || field.type === "range" ? " of no more than " : " no later than ") + field.max + ".";
    if (v.tooShort) return "Use at least " + field.minLength + " characters for " + label + ".";
    if (v.tooLong) return "Use no more than " + field.maxLength + " characters for " + label + ".";
    if (v.patternMismatch) return "Enter " + label + " in the required format." + (field.title ? " " + field.title : "");
    if (v.stepMismatch) return "Enter " + label + " in increments of " + (field.step || "1") + (field.min ? " starting at " + field.min : "") + ".";
    if (v.customError) return field.validationMessage;
    return "Check " + label + " and try again.";
  }

  function validationState(form) {
    if (!validationStates.has(form)) validationStates.set(form, { fields: new Map(), summary: null, pending: false });
    return validationStates.get(form);
  }

  function clearValidationField(state, field) {
    var record = state.fields.get(field);
    if (!record) return;
    record.message.remove();
    if (record.moneyWords) record.moneyWords.hidden = false;
    var described = (field.getAttribute("aria-describedby") || "").split(/\s+/).filter(function (id) { return id && id !== record.message.id; });
    if (described.length) field.setAttribute("aria-describedby", described.join(" "));
    else field.removeAttribute("aria-describedby");
    if (record.invalid === null) field.removeAttribute("aria-invalid");
    else field.setAttribute("aria-invalid", record.invalid);
    state.fields.delete(field);
  }

  function showValidationField(state, field) {
    var record = state.fields.get(field);
    if (!record) {
      if (!field.id) field.id = "validation-field-" + (++validationID);
      var message = document.createElement("span");
      message.className = "field-error-message client-field-error";
      message.id = "validation-message-" + (++validationID);
      var label = field.closest("label");
      var moneyField = field.closest(".money-field");
      var moneyWords = moneyField && moneyField.querySelector(".in-words");
      // Keep one inline message inside the money field's column. Inserting a
      // sibling of that column creates a narrow extra cell in form grids.
      if (moneyField) moneyField.appendChild(message);
      else (label || field).insertAdjacentElement("afterend", message);
      if (moneyWords) moneyWords.hidden = true;
      record = { message: message, moneyWords: moneyWords, invalid: field.getAttribute("aria-invalid") };
      state.fields.set(field, record);
      field.setAttribute("aria-describedby", ((field.getAttribute("aria-describedby") || "") + " " + message.id).trim());
    }
    record.message.textContent = validationMessage(field);
    field.setAttribute("aria-invalid", "true");
  }

  function renderValidationSummary(form, focus) {
    var state = validationState(form);
    state.fields.forEach(function (record, field) {
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
      var first = state.fields.keys().next().value;
      // Row editors have inputs outside their tiny form in the action cell.
      // Their summary belongs above the table, not inside that narrow cell.
      var table = !form.contains(first) && first.closest("table");
      if (table) table.parentNode.insertBefore(state.summary, table);
      else {
        var dialog = first.closest('[role="dialog"], dialog, .overlay');
        var container = dialog && !dialog.contains(form) ? dialog : form;
        container.insertBefore(state.summary, container.firstChild);
      }
    }
    state.summary.replaceChildren();
    var heading = document.createElement("h2");
    heading.textContent = "Check the highlighted fields";
    var intro = document.createElement("p");
    intro.textContent = "Nothing was submitted. Correct the following to continue.";
    var list = document.createElement("ul");
    var radioNames = new Set();
    state.fields.forEach(function (record, field) {
      if (field.type === "radio" && radioNames.has(field.name)) return;
      if (field.type === "radio") radioNames.add(field.name);
      var item = document.createElement("li");
      var link = document.createElement("a");
      link.href = "#" + field.id;
      link.textContent = record.message.textContent;
      link.addEventListener("click", function (event) {
        event.preventDefault();
        // A required field inside a collapsed accordion remains required.
        // Open only its accordion; do not remove or weaken its constraints.
        var section = field.closest(".acc-item, .pa-item");
        if (section) { var head = section.querySelector(HEAD_SELECTOR); if (head) setAccordion(head, true); }
        field.focus();
        field.scrollIntoView({ block: "center" });
      });
      item.appendChild(link); list.appendChild(item);
    });
    state.summary.appendChild(heading); state.summary.appendChild(intro); state.summary.appendChild(list);
    if (focus) { state.summary.focus(); state.summary.scrollIntoView({ block: "center" }); }
  }

  // htmx preview requests must pass the same validation gate as a submission.
  function validatePaymentEntry(form) {
    if (!form || !form.matches("[data-payment-entry]")) return true;
    var amount = form.querySelector('[name="amount"]');
    var reference = form.querySelector("[data-payment-reference]");
    if (amount) {
      var value = moneyValue(amount.value);
      var approved = form.querySelector("[data-approved]");
      var error = value.error || "";
      if (!error && approved && BigInt(value.paise) > BigInt(approved.getAttribute("data-approved"))) {
        error = "Enter an amount no greater than the remaining approved balance.";
      }
      amount.setCustomValidity(error);
    }
    if (reference) reference.setCustomValidity(reference.value.trim() ? "" : "Enter a transaction or payment reference; spaces alone are not a reference.");
    // Dismissed previews retain their controls until the next htmx swap.
    // Validate entry fields only; an unchosen old settlement is not an entry error.
    var valid = true;
    Array.prototype.forEach.call(form.elements, function (field) {
      if (!field.closest("#settle-mount") && typeof field.checkValidity === "function" && !field.checkValidity()) valid = false;
    });
    return valid;
  }

  document.addEventListener("click", function (event) {
    var button = event.target.closest && event.target.closest('button[formaction*="/settlement-preview"]');
    if (button && !validatePaymentEntry(button.form)) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);

  document.addEventListener("invalid", function (event) {
    var field = event.target;
    if (!field.form || !field.willValidate) return;
    event.preventDefault();
    var form = field.form;
    // Expose entry errors even if a stale/programmatic confirmation finds one.
    var sheet = form.querySelector("#settle-sheet");
    if (sheet && !sheet.contains(field)) closeDialog(sheet);
    var state = validationState(form);
    // Save the name before an inline error is inserted into a wrapping label.
    validationLabel(field);
    if (field.type === "radio" && Array.from(state.fields.keys()).some(function (other) {
      return other.type === "radio" && other.name === field.name;
    })) return;
    showValidationField(state, field);
    if (!state.pending) {
      state.pending = true;
      setTimeout(function () { state.pending = false; if (form.isConnected) renderValidationSummary(form, true); }, 0);
    }
  }, true);

  function updateValidation(event) {
    var field = event.target;
    if (!field.form) return;
    var form = field.form;
    if (field.matches("[data-payment-reference]")) field.setCustomValidity(field.value.trim() ? "" : "Enter a transaction or payment reference; spaces alone are not a reference.");
    // Run after conditional handlers have disabled irrelevant controls. Only
    // revisit reported errors; untouched fields stay quiet while typing.
    setTimeout(function () {
      if (!form.isConnected) return;
      if (validationStates.has(form) && validationState(form).fields.size) renderValidationSummary(form, false);
      var urgency = form.querySelector('[name="urgency_reason"][data-required-when-visible]');
      if (urgency && (!urgency.required || urgency.value.trim())) {
        Array.prototype.forEach.call(document.querySelectorAll('[data-server-error-for="urgency_reason"]'), function (banner) { banner.remove(); });
      }
    }, 0);
  }
  document.addEventListener("input", updateValidation);
  document.addEventListener("change", updateValidation);
  document.addEventListener("reset", function (event) {
    var state = validationStates.get(event.target);
    if (!state) return;
    state.fields.forEach(function (record, field) { clearValidationField(state, field); });
    if (state.summary) state.summary.remove();
    state.summary = null;
  });

  /* ---------------------------------------------------------------- */
  /* Delegated clicks                                                  */
  /* ---------------------------------------------------------------- */

  document.addEventListener("click", function (event) {
    var target = event.target;
    if (!target || typeof target.closest !== "function") return;

    /* Mobile "More" tab */
    var moreTab = target.closest(".js-more");
    if (moreTab) {
      event.preventDefault();
      openDialog(document.querySelector(".more-sheet"), moreTab);
      return;
    }
    var moreClose = target.closest(".ms-close");
    if (moreClose) {
      event.preventDefault();
      closeDialog(moreClose.closest(".more-sheet") || document.querySelector(".more-sheet"));
      return;
    }

    /* Accordions */
    var head = target.closest(HEAD_SELECTOR);
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
    var option = target.closest(".combo-list [data-id]");
    if (option) {
      var field = option.closest(".field");
      var hidden = field && field.querySelector("[data-combo-value]");
      if (hidden) {
        event.preventDefault();
        hidden.value = option.getAttribute("data-id");
        var visible = field.querySelector(".combo-input");
        if (visible) visible.value = option.getAttribute("data-name") || "";
        var list = option.closest(".combo-list");
        if (list && list.parentNode) list.parentNode.innerHTML = "";
        return;
      }
    }

    /* Declarative overlays */
    var opener = target.closest("[data-open]");
    if (opener) {
      event.preventDefault();
      openDialog(document.getElementById(opener.getAttribute("data-open")), opener);
      return;
    }
    var closer = target.closest("[data-close]");
    if (closer) {
      event.preventDefault();
      closeDialog(document.getElementById(closer.getAttribute("data-close")));
      return;
    }

    /* Backdrop: only the overlay itself, never anything inside the sheet. */
    if (target.classList && target.classList.contains("overlay")) {
      closeDialog(target);
      return;
    }

    /* Variance grid */
    var projectToggle = target.closest("[data-project-toggle]");
    if (projectToggle) {
      toggleProject(projectToggle);
    }
  });

  /* Keyboard parity for any accordion head that is not a real <button>. */
  document.addEventListener("keydown", function (event) {
    if (event.key !== "Enter" && event.key !== " " && event.key !== "Spacebar") return;
    var target = event.target;
    if (!target || typeof target.closest !== "function") return;
    var head = target.closest(HEAD_SELECTOR);
    if (!head || head.tagName === "BUTTON") return;
    event.preventDefault();
    toggleAccordion(head);
  });

  document.addEventListener("change", function () {
    syncConditionals(document);
  });

  /* ---------------------------------------------------------------- */
  /* Roles matrix — twin renderings, one form                          */
  /* ---------------------------------------------------------------- */

  /* Both renderings live in one <form>, so exactly one may submit. The
     desktop table is authoritative without JS — it scrolls inside
     .table-wrap at any width — and below 860px we hand over to the
     accordion. Nothing here decides what may be granted: every submitted
     cell and permission is re-validated against the canonical vocabulary
     in Go. */
  function initRoleMatrix(root) {
    var form = root.querySelector ? root.querySelector("#role-form") : null;
    if (!form || form.getAttribute("data-matrix-bound") === "1") return;
    var desktop = form.querySelector(".perm-table");
    var mobile = form.querySelector(".perm-acc");
    if (!desktop || !mobile) return;
    form.setAttribute("data-matrix-bound", "1");

    var narrow = window.matchMedia("(max-width: 860px)");
    var setDisabled = function (scope, off) {
      Array.prototype.forEach.call(scope.querySelectorAll("input"), function (input) {
        input.disabled = off;
      });
    };
    var handOver = function () {
      setDisabled(desktop, narrow.matches);
      setDisabled(mobile, !narrow.matches);
    };
    if (narrow.addEventListener) {
      narrow.addEventListener("change", handOver);
    } else if (narrow.addListener) {
      narrow.addListener(handOver);
    }
    handOver();

    /* A cell owns every canonical action behind it: toggling it toggles the
       Advanced checkboxes it covers, so clearing a cell really does revoke.
       The sync runs both ways. An Advanced box unticked under a ticked cell
       leaves the cell indeterminate and unchecked, so the cell is not
       submitted and the boxes are what the server saves; the server draws
       that partial state on load as data-partial. Both renderings are kept
       equal so a resize mid-edit loses nothing. */
    var all = function (selector) {
      return Array.prototype.slice.call(form.querySelectorAll(selector));
    };
    var syncCell = function (value) {
      var boxes = all('input[name="perm"][data-cell="' + window.CSS.escape(value) + '"]');
      var ticked = boxes.filter(function (box) { return box.checked; }).length;
      all('input[name="cell"][value="' + window.CSS.escape(value) + '"]').forEach(function (cell) {
        cell.checked = boxes.length > 0 && ticked === boxes.length;
        cell.indeterminate = ticked > 0 && !cell.checked;
      });
    };
    /* The accordion badge counts the row's held grants out of what it maps. */
    var syncBadges = function () {
      Array.prototype.forEach.call(mobile.querySelectorAll(".pa-item"), function (item) {
        var badge = item.querySelector(".pa-head .n");
        var boxes = item.querySelectorAll('input[name="perm"]');
        var ticked = item.querySelectorAll('input[name="perm"]:checked').length;
        if (badge) badge.textContent = ticked + " of " + boxes.length;
      });
    };
    all('input[name="cell"][data-partial]').forEach(function (cell) {
      cell.indeterminate = true;
    });
    form.addEventListener("change", function (event) {
      var input = event.target;
      if (!input || !input.matches) return;
      if (input.matches('input[name="cell"]')) {
        all('input[name="perm"][data-cell="' + window.CSS.escape(input.value) + '"]').forEach(function (box) {
          box.checked = input.checked;
        });
        syncCell(input.value);
        syncBadges();
      } else if (input.matches('input[name="perm"]')) {
        all('input[name="perm"][value="' + window.CSS.escape(input.value) + '"]').forEach(function (box) {
          box.checked = input.checked;
        });
        if (input.getAttribute("data-cell")) syncCell(input.getAttribute("data-cell"));
        syncBadges();
      } else if (input.matches('.perm-scope input[type="radio"]')) {
        /* The pills are lit by .is-on, which the server sets for the saved
           scope; the highlight follows the selection from here on. */
        var group = input.closest(".perm-scope");
        if (!group) return;
        Array.prototype.forEach.call(group.querySelectorAll("label"), function (label) {
          var radio = label.querySelector("input");
          label.classList.toggle("is-on", !!radio && radio.checked);
        });
      }
    });
  }

  /* ---------------------------------------------------------------- */
  /* Live difference against the approved ceiling                      */
  /* ---------------------------------------------------------------- */

  /* Display only. The store enforces "paid may never exceed approved" (G13);
     this just lets the accountant see the refusal coming instead of meeting it
     after they press confirm. Reuses the money field's own grouping so there is
     one formatter on the page, not two. */
  function initDifferenceBanner(root) {
    Array.prototype.forEach.call(root.querySelectorAll("[data-approved]"), function (source) {
      if (source.getAttribute("data-diff-bound") === "1") return;
      var approved = BigInt(source.getAttribute("data-approved") || "0");
      var paid = document.getElementById("amount");
      var banner = document.getElementById("diff-banner");
      var text = document.getElementById("diff-text");
      if (!paid || !banner || !text) return;
      source.setAttribute("data-diff-bound", "1");

      function sync() {
        var value = moneyValue(paid.value);
        if (value.error) { banner.hidden = true; return; }
        banner.hidden = false;
        var diff = approved - BigInt(value.paise);
        banner.className = "banner " + (diff === 0n ? "good" : diff > 0n ? "warn" : "bad");
        if (diff === 0n) {
          text.textContent = "Matches the approved amount exactly";
        } else {
          var absolute = diff < 0n ? -diff : diff;
          var rupees = indianGroup((absolute / 100n).toString() + "." + (absolute % 100n).toString().padStart(2, "0"));
          text.textContent = diff > 0n
            ? "₹" + rupees + " less than approved"
            : "₹" + rupees + " more than approved — not allowed";
        }
      }

      paid.addEventListener("input", sync);
      paid.addEventListener("blur", sync);
      sync();
    });
  }

  /* ---------------------------------------------------------------- */
  /* [data-follows-select] — a label that names the chosen option      */
  /* ---------------------------------------------------------------- */

  /* An element carrying data-follows-select="<select id>" and
     data-follows-text="… {name} …" re-reads its text from the selected
     option whenever the select changes. The server renders the label for
     the stored value, so the page reads correctly without this; it exists
     because "Save and notify Mona" beside a select that now says Max is a
     promise the submit will not keep. */
  function initFollowSelects(root) {
    Array.prototype.forEach.call(root.querySelectorAll("[data-follows-select]"), function (label) {
      if (label.dataset.followsBound) return;
      var select = document.getElementById(label.getAttribute("data-follows-select"));
      var text = label.getAttribute("data-follows-text");
      if (!select || !text) return;
      label.dataset.followsBound = "1";

      function sync() {
        var option = select.options[select.selectedIndex];
        if (!option) return;
        label.textContent = text.replace("{name}", option.textContent.trim());
      }

      select.addEventListener("change", sync);
      sync();
    });
  }

  /* ---------------------------------------------------------------- */
  /* Boot, and re-boot after every htmx swap                           */
  /* ---------------------------------------------------------------- */

  /* A sheet the server renders already open — a form it refused, sent back
     with the reader's input and an inline error — joins the stack as if the
     reader had opened it, so Escape, the focus trap and Cancel all work. */
  function openServedDialogs(root) {
    Array.prototype.forEach.call(root.querySelectorAll(".overlay[data-open-on-load]:not([hidden])"), function (el) {
      var opener = Array.prototype.find.call(root.querySelectorAll("[data-open]"), function (button) {
        return button.getAttribute("data-open") === el.id && button.getClientRects().length > 0;
      });
      openDialog(el, opener || null);
    });
  }

  function focusErrorSummary(root) {
    var summary = root.querySelector(".error-summary:not(.client-error-summary):not([data-focused])");
    if (!summary || !summary.getClientRects().length) return;
    summary.setAttribute("data-focused", "true");
    summary.setAttribute("tabindex", "-1");
    summary.focus();
  }

  function init() {
    initAccordions(document);
    syncConditionals(document);
    Array.prototype.forEach.call(document.forms, function (form) {
      if (validationStates.has(form)) renderValidationSummary(form, false);
    });
    initMoneyFields(document);
    initDifferenceBanner(document);
    initRoleMatrix(document);
    initFollowSelects(document);
    reopenRefusedSheets(document);
    openServedDialogs(document);
    focusErrorSummary(document);
  }

  /* A sheet whose form the server refused comes back with data-reopen and the
     admin's own input, and opens again through openDialog so focus and Escape
     behave as if they had opened it. The attribute is spent on first use, so a
     later htmx swap never reopens a sheet the user has closed. */
  function reopenRefusedSheets(root) {
    var sheets = root.querySelectorAll ? root.querySelectorAll(".overlay[data-reopen]") : [];
    Array.prototype.forEach.call(sheets, function (el) {
      el.removeAttribute("data-reopen");
      openDialog(el, null);
    });
  }

  /* A filter form that pushes its URL (the grid's) can fire the same filters
     more than once for one gesture: Enter in the search box commits a change
     and then submits, and the live search fires again when its pause runs out.
     Each extra request only swaps in the same rows and pushes a duplicate
     history entry, so a request whose filters were the last ones sent is
     dropped, and so is a live one whose filters the page already shows. Back
     and Forward forget the last one sent, since the page then shows whatever
     the server rendered for that URL. */
  var lastFilters = new WeakMap();
  document.addEventListener("htmx:configRequest", function (event) {
    var form = event.detail && event.detail.elt;
    if (!form || !form.matches || !form.matches("form[data-skip-shown-filters]")) return;
    var trigger = event.detail.triggeringEvent;
    var wanted = new URLSearchParams(new FormData(form)).toString();
    var shown = form.getAttribute("action") === window.location.pathname &&
      new URLSearchParams(window.location.search).toString() === wanted;
    if (lastFilters.get(form) === wanted || (shown && !(trigger && trigger.type === "submit"))) {
      event.preventDefault();
      return;
    }
    lastFilters.set(form, wanted);
  });
  document.addEventListener("htmx:historyRestore", function () { lastFilters = new WeakMap(); });

  document.addEventListener("htmx:afterSettle", function () {
    Array.prototype.forEach.call(document.forms, function (form) {
      if (validationStates.has(form)) renderValidationSummary(form, false);
    });
  });
  document.addEventListener("htmx:afterSwap", function () { init(); });
  document.addEventListener("htmx:load", function () { init(); });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
