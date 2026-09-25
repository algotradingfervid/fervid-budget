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
    el.hidden = false;
    openDialogs.push({ el: el, opener: opener || null });

    var list = focusables(el);
    if (list.length) {
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
    /* Focus goes back where the user left it, never to the top of the page. */
    if (record.opener && document.contains(record.opener)) record.opener.focus();
  }

  function topDialog() {
    return openDialogs.length ? openDialogs[openDialogs.length - 1].el : null;
  }

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
    });
  }

  /* ---------------------------------------------------------------- */
  /* Money field — Indian grouping and the amount in words             */
  /* ---------------------------------------------------------------- */

  function moneyInputs(root) {
    return Array.prototype.filter.call(root.querySelectorAll(".money-field input"), function (input) {
      var type = (input.getAttribute("type") || "text").toLowerCase();
      return type === "text" || type === "tel" || type === "search" || type === "number";
    });
  }

  /* Only digits and a single decimal point survive. */
  function rawAmount(value) {
    var cleaned = String(value == null ? "" : value).replace(/[^\d.]/g, "");
    var dot = cleaned.indexOf(".");
    if (dot === -1) return cleaned;
    return cleaned.slice(0, dot + 1) + cleaned.slice(dot + 1).replace(/\./g, "");
  }

  function indianGroup(raw) {
    var parts = raw.split(".");
    var whole = parts[0].replace(/^0+(?=\d)/, "");
    var frac = parts.length > 1 ? parts[1].slice(0, 2) : null;
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
    if (crore) out.push(three(crore) + " crore");
    if (lakh) out.push(three(lakh) + " lakh");
    if (thousand) out.push(three(thousand) + " thousand");
    if (n) out.push(three(n));
    var s = out.join(" ");
    return s.charAt(0).toUpperCase() + s.slice(1);
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
    var raw = rawAmount(before);
    var amount = parseFloat(raw);

    if (!raw || isNaN(amount)) {
      if (input.value !== raw) input.value = raw;
      if (out) {
        out.textContent = EMPTY_WORDS;
        out.classList.add("empty");
      }
      return;
    }

    var grouped = indianGroup(raw);
    if (grouped !== before) {
      var caret = keepCaret && input.selectionStart != null
        ? significantBefore(before, input.selectionStart)
        : -1;
      input.value = grouped;
      if (caret !== -1) setCaret(input, offsetAfterSignificant(grouped, caret));
    }
    if (out) {
      out.textContent = inWords(amount) + " rupees only";
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
    moneyInputs(form).forEach(function (input) {
      input.value = rawAmount(input.value);
    });
  }, true);

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
       Advanced checkboxes it covers, so clearing a cell really does revoke. */
    form.addEventListener("change", function (event) {
      var cell = event.target;
      if (!cell || !cell.matches || !cell.matches('input[name="cell"]')) return;
      var selector = 'input[name="perm"][data-cell="' + window.CSS.escape(cell.value) + '"]';
      Array.prototype.forEach.call(form.querySelectorAll(selector), function (box) {
        box.checked = cell.checked;
      });
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
      var approved = Math.round(parseFloat(rawAmount(source.getAttribute("data-approved")) || "0"));
      var paid = document.getElementById("amount");
      var banner = document.getElementById("diff-banner");
      var text = document.getElementById("diff-text");
      if (!paid || !banner || !text) return;
      source.setAttribute("data-diff-bound", "1");

      function sync() {
        var entered = parseFloat(rawAmount(paid.value));
        var paise = isNaN(entered) ? 0 : Math.round(entered * 100);
        var diff = approved - paise;
        banner.className = "banner " + (diff === 0 ? "good" : diff > 0 ? "warn" : "bad");
        if (diff === 0) {
          text.textContent = "Matches the approved amount exactly";
        } else {
          var rupees = indianGroup((Math.abs(diff) / 100).toFixed(2));
          text.textContent = diff > 0
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
  /* Boot, and re-boot after every htmx swap                           */
  /* ---------------------------------------------------------------- */

  function init() {
    initAccordions(document);
    syncConditionals(document);
    initMoneyFields(document);
    initDifferenceBanner(document);
    initRoleMatrix(document);
    reopenRefusedSheets(document);
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

  document.addEventListener("htmx:afterSwap", function () { init(); });
  document.addEventListener("htmx:load", function () { init(); });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
