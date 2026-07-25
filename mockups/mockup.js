/* ==========================================================================
   Fervid Budget — mockup shell
   Builds the app chrome around each screen's content, switches persona and
   device, and keeps that state in the query string so the files work when
   opened straight from disk (file://).
   ========================================================================== */

(function () {
  "use strict";

  var PERSONAS = {
    employee: { name: "Arun Mehta", role: "Employee · Site operations", initials: "AM" },
    manager:  { name: "Kavita Rao", role: "Manager · Operations",       initials: "KR" },
    accounts: { name: "Priya Nair", role: "Accounts",                   initials: "PN" },
    admin:    { name: "Naren Dhupati", role: "Administrator",           initials: "ND" }
  };

  var ALL = ["employee", "manager", "accounts", "admin"];

  /* nav item: key, label, href, icon, personas, badge */
  var NAV = [
    { group: null, items: [
      { key: "dashboard", label: "Home", href: "dashboard.html", icon: "⌂", personas: ALL }
    ]},
    { group: "Requests", items: [
      { key: "requests-list",   label: "My requests",    href: "requests-list.html",          icon: "▤", personas: ALL },
      { key: "approvals",       label: "Approvals",      href: "approvals-list.html",         icon: "✓", personas: ["manager", "admin"], badge: "4" },
      { key: "accounts-queue",  label: "Accounts queue", href: "accounts-queue.html",         icon: "₹", personas: ["accounts", "admin"], badge: "7" },
      { key: "recoverables",    label: "Recoverables",   href: "recoverables-dashboard.html", icon: "↩", personas: ["manager", "accounts", "admin"] }
    ]},
    { group: "Payments", items: [
      { key: "payments", label: "Payments ledger", href: "payments-ledger.html", icon: "▦", personas: ["manager", "accounts", "admin"] }
    ]},
    { group: "Budget", items: [
      { key: "variance-grid", label: "Variance grid", href: "variance-grid.html", icon: "▥", personas: ALL },
      { key: "budgets",       label: "Budgets",       href: "budgets.html",       icon: "◴", personas: ["accounts", "admin"] },
      { key: "monthly-plans", label: "Monthly plans", href: "monthly-plans.html", icon: "◷", personas: ["admin"] }
    ]},
    { group: "Reports", items: [
      { key: "reports", label: "Reports", href: "reports.html", icon: "⤓", personas: ALL }
    ]},
    { group: "Masters", items: [
      { key: "vendors",  label: "Vendors",  href: "vendors-list.html",   icon: "◫", personas: ["accounts", "admin"] },
      { key: "projects", label: "Projects", href: "masters-projects.html", icon: "▣", personas: ["admin"] },
      { key: "heads",    label: "Heads",    href: "masters-heads.html",    icon: "≡", personas: ["admin"] }
    ]},
    { group: "Admin", items: [
      { key: "users",         label: "Users",                href: "admin-users.html",         icon: "◍", personas: ["admin"] },
      { key: "roles",         label: "Roles & permissions",  href: "admin-roles.html",         icon: "⚿", personas: ["admin"] },
      { key: "configuration", label: "Configuration",        href: "admin-configuration.html", icon: "⚙", personas: ["admin"] },
      { key: "notif-admin",   label: "Notification rules",   href: "admin-notifications.html", icon: "✉", personas: ["admin"] },
      { key: "audit",         label: "Audit log",            href: "audit.html",               icon: "◎", personas: ["admin"] },
      { key: "backups",       label: "Backups",              href: "backups.html",             icon: "⇪", personas: ["admin"] }
    ]},
    { group: "Coming soon", items: [
      { key: "invoices",  label: "Invoices",         href: "", icon: "▧", personas: ALL, soon: true },
      { key: "receipts",  label: "Payments received", href: "", icon: "↧", personas: ALL, soon: true },
      { key: "inventory", label: "Inventory",        href: "", icon: "▨", personas: ALL, soon: true },
      { key: "po",        label: "Purchase orders",  href: "", icon: "▩", personas: ALL, soon: true }
    ]}
  ];

  /* mobile tab bars: four slots plus the centre action */
  var TABS = {
    employee: {
      left:  [{ label: "Home", href: "dashboard.html", icon: "⌂", key: "dashboard" },
              { label: "Requests", href: "requests-list.html", icon: "▤", key: "requests-list" }],
      fab:   { label: "New", href: "request-new-type.html", key: "new" },
      right: [{ label: "Budget", href: "variance-grid.html", icon: "▥", key: "variance-grid" },
              { label: "More", href: "#more", icon: "⋯", key: "more" }]
    },
    manager: {
      left:  [{ label: "Home", href: "dashboard.html", icon: "⌂", key: "dashboard" },
              { label: "Requests", href: "requests-list.html", icon: "▤", key: "requests-list" }],
      fab:   { label: "Approve", href: "approvals-list.html", key: "approvals", badge: "4" },
      right: [{ label: "Payments", href: "payments-ledger.html", icon: "▦", key: "payments" },
              { label: "More", href: "#more", icon: "⋯", key: "more" }]
    },
    accounts: {
      left:  [{ label: "Home", href: "dashboard.html", icon: "⌂", key: "dashboard" },
              { label: "Queue", href: "accounts-queue.html", icon: "₹", key: "accounts-queue" }],
      fab:   { label: "Pay", href: "payment-request-picker.html", key: "pay" },
      right: [{ label: "Payments", href: "payments-ledger.html", icon: "▦", key: "payments" },
              { label: "More", href: "#more", icon: "⋯", key: "more" }]
    },
    admin: {
      left:  [{ label: "Home", href: "dashboard.html", icon: "⌂", key: "dashboard" },
              { label: "Requests", href: "requests-list.html", icon: "▤", key: "requests-list" }],
      fab:   { label: "New", href: "request-new-type.html", key: "new" },
      right: [{ label: "Payments", href: "payments-ledger.html", icon: "▦", key: "payments" },
              { label: "More", href: "#more", icon: "⋯", key: "more" }]
    }
  };

  /* ---------- state ---------- */

  var params = new URLSearchParams(window.location.search);
  var persona = params.get("persona");
  var device = params.get("device");

  if (ALL.indexOf(persona) === -1) {
    persona = document.body.getAttribute("data-persona-default") || "employee";
  }
  if (device !== "mobile" && device !== "desktop") {
    device = window.innerWidth < 860 ? "mobile" : "desktop";
  }
  document.documentElement.setAttribute("data-device", device);
  document.documentElement.setAttribute("data-persona", persona);

  var inScreens = /\/screens\//.test(window.location.pathname);
  var toRoot = inScreens ? "../" : "";
  var toScreens = inScreens ? "" : "screens/";

  function qs(extra) {
    var p = new URLSearchParams();
    p.set("persona", extra && extra.persona ? extra.persona : persona);
    p.set("device", extra && extra.device ? extra.device : device);
    return "?" + p.toString();
  }

  function screenHref(file) {
    if (!file) return "#";
    if (file.charAt(0) === "#") return file;
    return toScreens + file + qs();
  }

  function el(tag, cls, html) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (html != null) n.innerHTML = html;
    return n;
  }

  /* ---------- prototype bar ---------- */

  function buildProtoBar(meta) {
    var bar = el("div", "proto-bar");
    var opts = ALL.map(function (p) {
      return '<option value="' + p + '"' + (p === persona ? " selected" : "") + ">" +
        p.charAt(0).toUpperCase() + p.slice(1) + " · " + PERSONAS[p].name + "</option>";
    }).join("");

    bar.innerHTML =
      '<a class="pb-home" href="' + toRoot + "index.html" + qs() + '">' +
        '<span class="pb-sq">F</span><span>Fervid Budget mockups</span></a>' +
      '<span class="pb-screen">' + (meta.file || "") + "</span>" +
      '<span class="pb-spacer"></span>' +
      '<label for="pb-persona">Viewing as</label>' +
      '<select id="pb-persona">' + opts + "</select>" +
      '<div class="proto-seg" role="group" aria-label="Device">' +
        '<button type="button" data-device="mobile" aria-pressed="' + (device === "mobile") + '">Mobile</button>' +
        '<button type="button" data-device="desktop" aria-pressed="' + (device === "desktop") + '">Desktop</button>' +
      "</div>";

    bar.querySelector("#pb-persona").addEventListener("change", function (e) {
      window.location.search = qs({ persona: e.target.value });
    });
    Array.prototype.forEach.call(bar.querySelectorAll(".proto-seg button"), function (b) {
      b.addEventListener("click", function () {
        window.location.search = qs({ device: b.getAttribute("data-device") });
      });
    });
    return bar;
  }

  /* ---------- sidebar ---------- */

  function visibleGroups() {
    return NAV.map(function (g) {
      return {
        group: g.group,
        items: g.items.filter(function (it) { return it.soon || it.personas.indexOf(persona) !== -1; })
      };
    }).filter(function (g) { return g.items.length; });
  }

  function buildSidebar(meta) {
    var side = el("aside", "sidebar");
    var who = PERSONAS[persona];
    var html =
      '<div class="side-brand"><span class="sq">F</span>' +
      "<span><b>Fervid Budget</b><small>Smart Solutions</small></span></div>";

    visibleGroups().forEach(function (g) {
      if (g.group) html += '<div class="sgroup">' + g.group + "</div>";
      html += '<nav class="side-nav">';
      g.items.forEach(function (it) {
        var active = it.key === meta.nav ? " active" : "";
        if (it.soon) {
          html += '<a class="soon" href="#" aria-disabled="true"><span class="ico">' + it.icon +
            "</span>" + it.label + '<span class="n">Soon</span></a>';
        } else {
          html += '<a class="' + active.trim() + '" href="' + screenHref(it.href) + '">' +
            '<span class="ico">' + it.icon + "</span>" + it.label +
            (it.badge ? '<span class="n">' + it.badge + "</span>" : "") + "</a>";
        }
      });
      html += "</nav>";
    });

    html +=
      '<div class="side-user"><span class="avatar">' + who.initials + "</span>" +
      "<span><strong>" + who.name + "</strong><small>" + who.role + "</small></span></div>";
    side.innerHTML = html;
    return side;
  }

  /* ---------- mobile chrome ---------- */

  function buildMobileTop(meta) {
    var bar = el("header", "m-topbar");
    var back = meta.back
      ? '<a class="m-back" href="' + screenHref(meta.back) + '" aria-label="Back">‹</a>'
      : '<a class="m-back" href="' + screenHref("dashboard.html") + '" aria-label="Home">⌂</a>';
    bar.innerHTML = back +
      '<div class="m-title"><b>' + (meta.title || "Fervid Budget") + "</b>" +
      (meta.sub ? "<small>" + meta.sub + "</small>" : "") + "</div>" +
      '<div class="m-actions">' +
        (meta.action
          ? '<a class="m-cta" href="' + (meta.actionHref ? screenHref(meta.actionHref) : "#") + '">' + meta.action + "</a>"
          : "") +
        '<a class="m-icon" href="' + screenHref("notifications.html") + '" aria-label="Notifications">✉<span class="dot">3</span></a>' +
      "</div>";
    return bar;
  }

  function buildTabbar(meta) {
    var conf = TABS[persona];
    var bar = el("nav", "tabbar");
    var html = "";
    conf.left.forEach(function (t) {
      html += '<a href="' + screenHref(t.href) + '" class="' + (t.key === meta.nav ? "active" : "") + '">' +
        '<span class="ti">' + t.icon + "</span>" + t.label + "</a>";
    });
    html += '<a class="fab-wrap" href="' + screenHref(conf.fab.href) + '">' +
      '<span class="fab">＋</span>' + conf.fab.label + "</a>";
    conf.right.forEach(function (t) {
      var href = t.href === "#more" ? "#more" : screenHref(t.href);
      html += '<a href="' + href + '" class="' + (t.key === meta.nav ? "active" : "") +
        (t.href === "#more" ? " js-more" : "") + '">' +
        '<span class="ti">' + t.icon + "</span>" + t.label + "</a>";
    });
    bar.innerHTML = html;
    return bar;
  }

  function buildMoreSheet() {
    var sheet = el("div", "more-sheet");
    sheet.hidden = true;
    var who = PERSONAS[persona];
    var html = '<div class="ms-head"><b>' + who.name + "</b>" +
      '<button type="button" class="ms-close" aria-label="Close">✕</button></div>';
    visibleGroups().forEach(function (g) {
      html += '<div class="ms-group">' + (g.group || "Overview") + "</div>";
      html += '<div class="ms-list">';
      g.items.forEach(function (it) {
        if (it.soon) {
          html += '<a class="soon" href="#"><span class="ico">' + it.icon + "</span>" + it.label +
            '<span class="n">Soon</span></a>';
        } else {
          html += '<a href="' + screenHref(it.href) + '"><span class="ico">' + it.icon + "</span>" +
            it.label + (it.badge ? '<span class="n">' + it.badge + "</span>" : "") + "</a>";
        }
      });
      html += "</div>";
    });
    html += '<div class="ms-group">Session</div><div class="ms-list">' +
      '<a href="' + screenHref("login.html") + '"><span class="ico">⏻</span>Log out</a></div>';
    sheet.innerHTML = html;
    return sheet;
  }

  /* ---------- assemble ---------- */

  function boot() {
    var body = document.body;
    var meta = {
      title: body.getAttribute("data-title") || document.title,
      sub: body.getAttribute("data-sub") || "",
      nav: body.getAttribute("data-nav") || "",
      back: body.getAttribute("data-back") || "",
      action: body.getAttribute("data-action") || "",
      actionHref: body.getAttribute("data-action-href") || "",
      chrome: body.getAttribute("data-chrome") || "app",
      file: (window.location.pathname.split("/").pop() || "index.html")
    };

    var content = document.createDocumentFragment();
    while (body.firstChild) content.appendChild(body.firstChild);

    body.appendChild(buildProtoBar(meta));

    var stage = el("div", "stage");

    if (meta.chrome === "none") {
      stage.className = "stage plain";
      stage.appendChild(content);
      body.appendChild(stage);
    } else {
      var shell = el("div", "appshell");
      shell.appendChild(buildSidebar(meta));
      if (device === "mobile") shell.appendChild(buildMobileTop(meta));
      var page = el("main", "page");
      var inner = el("div", "page-inner");
      inner.appendChild(content);
      page.appendChild(inner);
      shell.appendChild(page);
      if (device === "mobile") {
        shell.appendChild(buildTabbar(meta));
        shell.appendChild(buildMoreSheet());
      }
      stage.appendChild(shell);
      body.appendChild(stage);
    }

    applyPersonaVisibility();
    rewriteLinks();
    wireInteractions();
  }

  /* Blocks marked data-for="manager,admin" only render for those personas.
     This is how one screen file serves four audiences. */
  function applyPersonaVisibility() {
    Array.prototype.forEach.call(document.querySelectorAll("[data-for]"), function (node) {
      var allowed = node.getAttribute("data-for").split(",").map(function (s) { return s.trim(); });
      if (allowed.indexOf(persona) === -1) node.remove();
    });
    Array.prototype.forEach.call(document.querySelectorAll("[data-not-for]"), function (node) {
      var blocked = node.getAttribute("data-not-for").split(",").map(function (s) { return s.trim(); });
      if (blocked.indexOf(persona) !== -1) node.remove();
    });
    Array.prototype.forEach.call(document.querySelectorAll("[data-persona-text]"), function (node) {
      node.textContent = PERSONAS[persona].name;
    });
  }

  /* Keep persona + device on every internal hop. */
  function rewriteLinks() {
    Array.prototype.forEach.call(document.querySelectorAll("a[href]"), function (a) {
      var href = a.getAttribute("href");
      if (!href || href.charAt(0) === "#" || /^https?:/.test(href) || href.indexOf("?persona=") !== -1) return;
      if (href.indexOf(".html") === -1) return;
      a.setAttribute("href", href + qs());
    });
  }

  function wireInteractions() {
    /* More sheet */
    document.addEventListener("click", function (e) {
      var more = e.target.closest(".js-more");
      if (more) {
        e.preventDefault();
        var sheet = document.querySelector(".more-sheet");
        if (sheet) sheet.hidden = false;
        return;
      }
      if (e.target.closest(".ms-close")) {
        var s = document.querySelector(".more-sheet");
        if (s) s.hidden = true;
        return;
      }

      /* Accordions */
      var accHead = e.target.closest(".acc-head, .pa-head");
      if (accHead) {
        var item = accHead.closest(".acc-item, .pa-item");
        if (item) item.classList.toggle("is-open");
        return;
      }

      /* Open / close overlays declaratively */
      var opener = e.target.closest("[data-open]");
      if (opener) {
        e.preventDefault();
        var target = document.getElementById(opener.getAttribute("data-open"));
        if (target) target.hidden = false;
        return;
      }
      var closer = e.target.closest("[data-close]");
      if (closer) {
        e.preventDefault();
        var t2 = document.getElementById(closer.getAttribute("data-close"));
        if (t2) t2.hidden = true;
        return;
      }
      if (e.target.classList && e.target.classList.contains("overlay")) {
        e.target.hidden = true;
      }
    });

    /* Radio card highlighting */
    Array.prototype.forEach.call(document.querySelectorAll(".choice"), function (group) {
      function sync() {
        Array.prototype.forEach.call(group.querySelectorAll("label"), function (l) {
          var r = l.querySelector('input[type="radio"], input[type="checkbox"]');
          l.classList.toggle("is-on", !!(r && r.checked));
        });
      }
      group.addEventListener("change", sync);
      sync();
    });

    /* Sections that appear based on a select or radio value */
    function syncConditionals() {
      Array.prototype.forEach.call(document.querySelectorAll("[data-when]"), function (node) {
        var rule = node.getAttribute("data-when").split(":");
        var all = document.querySelectorAll('[name="' + rule[0] + '"]');
        if (!all.length) return;
        var first = all[0], value;
        if (first.type === "checkbox") {
          value = first.checked ? "on" : "off";
        } else if (first.type === "radio") {
          var checked = document.querySelector('[name="' + rule[0] + '"]:checked');
          value = checked ? checked.value : "";
        } else {
          value = first.value;
        }
        node.hidden = rule[1].split("|").indexOf(value) === -1;
      });
    }
    document.addEventListener("change", syncConditionals);
    syncConditionals();

    /* Money field: live Indian grouping + words */
    Array.prototype.forEach.call(document.querySelectorAll(".money-field input"), function (input) {
      var out = input.closest(".money-field").querySelector(".in-words");
      function fmt() {
        var raw = input.value.replace(/[^\d.]/g, "");
        var n = parseFloat(raw);
        if (!raw || isNaN(n)) {
          if (out) { out.textContent = "Enter the amount you are requesting"; out.classList.add("empty"); }
          return;
        }
        input.value = indianGroup(raw);
        if (out) { out.textContent = inWords(n) + " rupees only"; out.classList.remove("empty"); }
      }
      input.addEventListener("blur", fmt);
      fmt();
    });
  }

  function indianGroup(raw) {
    var parts = raw.split(".");
    var n = parts[0].replace(/^0+(?=\d)/, "");
    var last3 = n.slice(-3);
    var rest = n.slice(0, -3);
    if (rest) last3 = rest.replace(/\B(?=(\d{2})+(?!\d))/g, ",") + "," + last3;
    return last3 + (parts[1] != null ? "." + parts[1].slice(0, 2) : "");
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

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
