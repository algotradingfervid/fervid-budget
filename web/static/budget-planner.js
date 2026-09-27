/*
 * Budget planner — the three-step "plan a month" page (/budgets/plan).
 *   1. Choose the month and a starting point (blank, or a copy of a saved month).
 *   2. Edit projects > expense heads > budget lines and their amounts.
 *   3. Review, then save.
 *
 * Everything the user does is kept in the in-memory `draft` until the final
 * form posts it as JSON in one submission; the server validates it again.
 *
 * Draft shape: draft.projects[] -> .heads[] -> .lines[].
 *   id   the database id; 0 means new (created when the budget is saved).
 *   key  a number used only on this page to build element ids.
 *   A line keeps the amount exactly as typed in `text`; readAmount() turns it
 *   into integer paise when a number is needed.
 *
 * All markup built here goes through esc() for every value that comes from
 * the user or the server, because it is inserted with innerHTML.
 */
(() => {
  'use strict';
  const root = document.getElementById('budget-planner');
  if (!root) return;
  const $ = id => document.getElementById('bp-' + id);
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[c]));
  const INR = new Intl.NumberFormat('en-IN', {style: 'currency', currency: 'INR', minimumFractionDigits: 2, maximumFractionDigits: 2});
  const money = paise => INR.format(paise / 100);
  // The largest amount, in paise, the server accepts for a line or a month.
  const MAX = 900000000000000;

  let config;
  try {
    config = JSON.parse(document.getElementById('budget-planner-data').textContent);
  } catch (_) {
    $('error').hidden = false;
    $('error').textContent = 'The budget planner could not load. Reload this page to try again.';
    $('continue').disabled = true;
    return;
  }

  let serial = 0;           // last key handed out
  let dirty = false;        // unsaved edits: the page warns before it is left
  let submitting = false;   // the save form has been sent
  let addingTo = null;      // project key the Add dialog adds a head to (null: adding a project)
  let signature = '';       // "mode|sourceMonth" the draft was built from; a change means rebuild
  let busy = false;         // a source month is being fetched

  const key = () => ++serial;

  /* Paise -> the "1234.50" text shown in an amount box. */
  const amountText = amount => {
    if (!Number.isSafeInteger(amount) || amount < 0) return String(amount ?? '');
    return Math.floor(amount / 100) + '.' + String(amount % 100).padStart(2, '0');
  };

  /* Typed text -> integer paise, or null if it is not a plain amount with up
     to two decimals within MAX. BigInt keeps large values exact. */
  const readAmount = text => {
    const value = String(text).trim();
    if (!/^\d+(?:\.\d{1,2})?$/.test(value) || value.length > 20) return null;
    const [whole, fraction = ''] = value.split('.');
    const paise = BigInt(whole) * 100n + BigInt(fraction.padEnd(2, '0'));
    return paise <= BigInt(MAX) ? Number(paise) : null;
  };

  /* Gives every project, head and line a key, and every line its amount text. */
  const hydrate = plan => ({
    ...plan,
    projects: (plan.projects || []).map(p => ({
      ...p,
      key: key(),
      heads: (p.heads || []).map(h => ({
        ...h,
        key: key(),
        lines: (h.lines || []).map(l => ({...l, key: key(), text: amountText(l.amount)})),
      })),
    })),
  });

  let draft = hydrate(config.draft || {month: '', projects: []});
  const locked = Boolean(draft.locked);
  const edit = Boolean(config.edit);
  // Persisted groups cannot be dropped by the planner. Removing a draft group
  // only removes it from this unsaved plan, never from the project/head catalog.
  const savedProjectIDs = new Set(config.savedProjectIds || (edit ? draft.projects.filter(p => p.id > 0).map(p => p.id) : []));
  const savedHeadIDs = new Set(config.savedHeadIds || (edit ? draft.projects.flatMap(p => p.heads).filter(h => h.id > 0).map(h => h.id) : []));
  const canRemoveProject = p => !locked && !p.readOnly && (!edit || !savedProjectIDs.has(p.id));
  const canRemoveHead = h => !locked && !h.readOnly && (!edit || !savedHeadIDs.has(h.id));
  const catalog = config.catalog || [];
  const sources = config.sources || [];
  const heads = () => draft.projects.flatMap(p => p.heads);
  const lines = () => heads().flatMap(h => h.lines);
  const isReadOnly = (p, h) => locked || p.readOnly || (h && h.readOnly);

  /* Sum of paise values; null if any is invalid or the total passes MAX. */
  const sum = values => {
    let total = 0;
    for (const value of values) {
      if (value === null || !Number.isSafeInteger(value) || value > MAX - total) return null;
      total += value;
    }
    return total;
  };
  const headTotal = h => sum(h.lines.map(l => readAmount(l.text)));
  const projectTotal = p => sum(p.heads.map(headTotal));
  const total = () => sum(draft.projects.map(projectTotal));
  const showMoney = value => value === null ? 'Check amounts' : money(value);
  const monthLabel = month => /^\d{4}-(0[1-9]|1[0-2])$/.test(month || '')
    ? new Intl.DateTimeFormat('en-IN', {month: 'long', year: 'numeric', timeZone: 'UTC'}).format(new Date(month + '-01T12:00:00Z'))
    : month;
  const mode = () => root.querySelector('input[name="bp-start"]:checked').value;

  /* Shows (or clears) the page error, then moves focus to the field at fault
     or to the error itself. */
  function error(message, focus) {
    $('error').textContent = message;
    $('error').hidden = !message;
    if (message && focus) reveal(focus);
    else if (message) $('error').focus();
  }

  /* Opens every collapsed group around an element, focuses it and scrolls it
     into view. */
  function reveal(element) {
    if (!element) {
      $('error').focus();
      return;
    }
    // Focusing a summary reveals its ancestors, but does not expand the item itself.
    const ancestor = element.tagName === 'SUMMARY' ? element.parentElement.parentElement : element.parentElement;
    for (let parent = ancestor; parent; parent = parent.parentElement) if (parent.tagName === 'DETAILS') parent.open = true;
    element.focus();
    element.scrollIntoView({block: 'center', behavior: 'instant'});
  }

  /* Shows step 1, 2 or 3 and marks it current in the step list. */
  function step(number) {
    for (let i = 1; i <= 3; i++) $('step-' + i).hidden = i !== number;
    root.querySelectorAll('.bp-steps li').forEach((li, i) => {
      if (i === number - 1) li.setAttribute('aria-current', 'step');
      else li.removeAttribute('aria-current');
    });
    $('title-' + number).focus();
    root.scrollIntoView({block: 'start', behavior: 'instant'});
  }

  /* The "March 2031 · Copied from February 2031" line and the grid link. */
  function context() {
    $('context').textContent = monthLabel(draft.month) + ' · ' + (draft.sourceMonth ? 'Copied from ' + monthLabel(draft.sourceMonth) : edit ? 'Editing the saved budget' : 'Starting blank');
    $('grid-link').href = '/grid?month=' + encodeURIComponent(draft.month || $('month').value);
  }

  function updateTotals() {
    draft.projects.forEach(p => {
      $('project-total-' + p.key).textContent = showMoney(projectTotal(p));
      p.heads.forEach(h => {
        $('head-total-' + h.key).textContent = showMoney(headTotal(h));
      });
    });
    $('live-total').textContent = showMoney(total());
    $('count').textContent = !draft.projects.length
      ? 'No items added yet'
      : heads().filter(h => headTotal(h) > 0).length + ' of ' + heads().length + ' expense heads have a budget · ' + draft.projects.length + ' projects';
  }

  /* ------------------------------------------------------------------ */
  /* Step 2 markup: one function per level (project > head > line)      */
  /* ------------------------------------------------------------------ */

  const EMPTY_PLAN_HTML = '<div class="bp-empty"><strong>Your budget starts here</strong><p class="muted">No projects, heads, or lines yet.<br>Add your first project to begin.</p></div>';
  const tag = text => `<span class="bp-tag">${text}</span>`;
  const openAttr = (open, id) => (open.has(id) ? 'open' : '');

  /* One budget line: description, amount and (when editable) Remove. */
  function lineHTML(p, h, l, index, readOnly) {
    const n = index + 1;
    const where = esc(p.name + ' / ' + h.name);
    const disabled = readOnly ? 'disabled' : '';
    return '<div class="bp-line">'
      + `<label for="bp-description-${l.key}">Line ${n} description`
      + `<input id="bp-description-${l.key}" data-description="${l.key}" aria-label="Line ${n} description for ${where}" placeholder="What is this budget for?" value="${esc(l.description)}" maxlength="240" required ${disabled}>`
      + '</label>'
      + `<label for="bp-amount-${l.key}">Amount (₹)`
      + `<input id="bp-amount-${l.key}" data-amount="${l.key}" aria-label="Line ${n} amount for ${where}" inputmode="decimal" autocomplete="off" value="${esc(l.text)}" required ${disabled}>`
      + '</label>'
      + (!readOnly ? `<button type="button" data-remove="${l.key}" aria-label="Remove line ${n} from ${where}">Remove</button>` : '')
      + '</div>';
  }

  /* One expense head: its summary row, its lines and its buttons. */
  function headHTML(p, h, open) {
    const readOnly = isReadOnly(p, h);
    const where = esc(p.name + ' / ' + h.name);
    const actions = !readOnly
      ? '<div class="bp-head-actions">'
        + `<button type="button" class="bp-add-line" data-add-line="${h.key}" aria-label="Add line to ${where}">＋ Add line</button>`
        + (canRemoveHead(h) ? `<button type="button" class="bp-remove-group" data-remove-head="${h.key}" aria-label="Remove draft head ${where}">Remove head from draft</button>` : '')
        + '</div>'
      : '';
    return `<details class="bp-head" id="bp-head-${h.key}" ${openAttr(open, 'bp-head-' + h.key)}>`
      + '<summary><span class="bp-item-label">'
      + `<b>${esc(h.name)}</b>`
      + (h.id === 0 ? tag('New head') : '')
      + (h.readOnly ? tag('Retired · read only') : '')
      + `<small>${h.lines.length} budget lines</small></span>`
      + `<output id="bp-head-total-${h.key}" aria-label="Total for head ${esc(h.name)}"></output></summary>`
      + h.lines.map((l, index) => lineHTML(p, h, l, index, readOnly)).join('')
      + (!h.lines.length ? '<p class="bp-line-hint">No budget lines yet. Add your first line below.</p>' : '')
      + actions
      + '</details>';
  }

  /* One project: its summary row, its heads and its buttons. */
  function projectHTML(p, open) {
    const projectReadOnly = isReadOnly(p);
    const foot = !projectReadOnly
      ? '<div class="bp-project-foot">'
        + `<button type="button" data-add-head="${p.key}" aria-label="Add head to ${esc(p.name)}">＋ Add expense head</button>`
        + (canRemoveProject(p) ? `<button type="button" class="bp-remove-group" data-remove-project="${p.key}" aria-label="Remove draft project ${esc(p.name)}">Remove project from draft</button>` : '')
        + '</div>'
      : '';
    return `<details class="bp-project" id="bp-project-${p.key}" ${openAttr(open, 'bp-project-' + p.key)}>`
      + '<summary><span class="bp-item-label">'
      + `<b>${esc(p.name)}</b>`
      + (p.id === 0 ? tag('New project') : '')
      + (p.readOnly ? tag('Retired · read only') : '')
      + `<small>${p.heads.length} expense heads</small></span>`
      + `<output id="bp-project-total-${p.key}" aria-label="Total for project ${esc(p.name)}"></output></summary>`
      + p.heads.map(h => headHTML(p, h, open)).join('')
      + (!p.heads.length ? '<p class="muted">No heads yet. Add your first expense head below.</p>' : '')
      + foot
      + '</details>';
  }

  /* Redraws step 2 from the draft, keeping open groups open. */
  function render() {
    const open = new Set(Array.from($('groups').querySelectorAll('details[open]'), d => d.id));
    $('outline-controls').hidden = !draft.projects.length;
    $('groups').innerHTML = draft.projects.length ? draft.projects.map(p => projectHTML(p, open)).join('') : EMPTY_PLAN_HTML;
    updateTotals();
    context();
  }

  /* ------------------------------------------------------------------ */
  /* Step 1: month and starting point                                   */
  /* ------------------------------------------------------------------ */

  function sourcePreview() {
    $('source').hidden = mode() !== 'copy';
    const source = sources.find(s => s.month === $('source-month').value);
    $('source-preview').textContent = source
      ? money(source.budget) + ' in the original month. Retired items are excluded from the copy; review its new total in the next step.'
      : 'No saved budgets are available to copy yet.';
  }

  /* Locks the step 1 controls while a source month is being fetched. */
  function setBusy(on) {
    busy = on;
    $('continue').disabled = on;
    $('continue').textContent = on ? 'Loading budget…' : 'Continue to amounts →';
    $('month').disabled = on || edit || locked;
    $('source-month').disabled = on;
    root.querySelectorAll('input[name="bp-start"]').forEach(input => { input.disabled = on; });
    if (!on) $('copy').disabled = !sources.length;
  }

  $('month').value = draft.month;
  $('month').disabled = edit || locked;
  $('source-month').innerHTML = sources.map(s => `<option value="${esc(s.month)}">${esc(monthLabel(s.month))}</option>`).join('');
  if (draft.sourceMonth && sources.some(s => s.month === draft.sourceMonth)) {
    $('source-month').value = draft.sourceMonth;
    $('copy').checked = true;
  }
  $('copy').disabled = !sources.length;
  root.querySelectorAll('input[name="bp-start"]').forEach(radio => radio.addEventListener('change', sourcePreview));
  $('source-month').addEventListener('change', sourcePreview);
  sourcePreview();

  /* "Continue": checks the month, (re)builds the draft if the starting point
     changed — fetching the source month when copying — and opens step 2. */
  $('continue').addEventListener('click', async () => {
    if (busy) return;
    const month = $('month').value;
    if (!/^(?!0000)\d{4}-(0[1-9]|1[0-2])$/.test(month)) {
      error('Choose a valid budget month.', $('month'));
      return;
    }
    const sourceMonth = mode() === 'copy' ? $('source-month').value : '';
    if (mode() === 'copy' && !sourceMonth) {
      error('Choose a source month or start with a blank budget.', $('source-month'));
      return;
    }
    if (sourceMonth === month) {
      error('Choose a different month from the budget you are copying.', $('month'));
      return;
    }
    const next = mode() + '|' + sourceMonth;
    if (signature !== next && dirty && !window.confirm('Changing the starting point will discard the project, head, and line changes in this unsaved draft. Continue?')) return;
    error('');
    if (signature !== next) {
      let projects = [];
      if (sourceMonth) {
        setBusy(true);
        try {
          const response = await fetch('/budgets/plan-data?month=' + encodeURIComponent(sourceMonth), {headers: {Accept: 'application/json'}});
          if (!response.ok) throw new Error('The source budget could not be loaded. It may no longer be available to your account.');
          const plan = await response.json();
          if (!plan || !Array.isArray(plan.projects)) throw new Error('The source budget returned an unexpected response. Reload this page and try again.');
          // Retired projects and heads are not copied.
          projects = plan.projects
            .filter(p => !p.readOnly)
            .map(p => ({...p, heads: (p.heads || []).filter(h => !h.readOnly)}))
            .filter(p => p.heads.length);
        } catch (problem) {
          error(problem.message || 'The source budget could not be loaded. Try again.');
          return;
        } finally {
          setBusy(false);
        }
      }
      draft = hydrate({...draft, projects, sourceMonth, month});
      dirty = projects.length > 0;
      signature = next;
    }
    if (draft.month !== month) dirty = true;
    draft.month = month;
    render();
    step(2);
  });

  /* ------------------------------------------------------------------ */
  /* Step 2: editing                                                     */
  /* ------------------------------------------------------------------ */

  $('expand').onclick = () => $('groups').querySelectorAll('details').forEach(d => { d.open = true; });
  $('collapse').onclick = () => $('groups').querySelectorAll('details').forEach(d => { d.open = false; });

  $('groups').addEventListener('input', event => {
    const target = event.target;
    if (target.dataset.amount) {
      const line = lines().find(l => l.key === Number(target.dataset.amount));
      if (!line) return;
      line.text = target.value;
      dirty = true;
      if (readAmount(line.text) === null) target.setAttribute('aria-invalid', 'true');
      else target.removeAttribute('aria-invalid');
      updateTotals();
    } else if (target.dataset.description) {
      const line = lines().find(l => l.key === Number(target.dataset.description));
      if (!line) return;
      line.description = target.value;
      dirty = true;
      target.removeAttribute('aria-invalid');
    }
  });

  /* Add line, Remove line, Add head, and the two "remove from draft" buttons. */
  $('groups').addEventListener('click', event => {
    const button = event.target.closest('button');
    if (!button || locked) return;
    if (button.dataset.addLine) {
      const head = heads().find(h => h.key === Number(button.dataset.addLine));
      if (!head || head.readOnly) return;
      const line = {key: key(), description: '', text: '0.00'};
      head.lines.push(line);
      dirty = true;
      render();
      reveal($('description-' + line.key));
    } else if (button.dataset.remove) {
      const id = Number(button.dataset.remove);
      const head = heads().find(h => h.lines.some(l => l.key === id));
      if (!head || head.readOnly) return;
      head.lines = head.lines.filter(l => l.key !== id);
      dirty = true;
      render();
      reveal(root.querySelector('[data-add-line="' + head.key + '"]'));
    } else if (button.dataset.addHead) {
      openItem(Number(button.dataset.addHead));
    } else if (button.dataset.removeHead) {
      const id = Number(button.dataset.removeHead);
      const project = draft.projects.find(p => p.heads.some(h => h.key === id));
      const head = project && project.heads.find(h => h.key === id);
      if (!head || !canRemoveHead(head)) return;
      if (head.lines.length && !window.confirm('Remove ' + head.name + ' and its budget lines from this unsaved draft? The expense head itself will not be deleted.')) return;
      project.heads = project.heads.filter(h => h.key !== id);
      dirty = true;
      error('');
      render();
      reveal(root.querySelector('[data-add-head="' + project.key + '"]'));
    } else if (button.dataset.removeProject) {
      const id = Number(button.dataset.removeProject);
      const project = draft.projects.find(p => p.key === id);
      if (!project || !canRemoveProject(project)) return;
      if (project.heads.length && !window.confirm('Remove ' + project.name + ' and its heads and budget lines from this unsaved draft? Existing projects and expense heads will not be deleted.')) return;
      draft.projects = draft.projects.filter(p => p.key !== id);
      dirty = true;
      error('');
      render();
      $('add-project').focus();
    }
  });

  /* ------------------------------------------------------------------ */
  /* The Add project / Add expense head dialog                           */
  /* ------------------------------------------------------------------ */

  function itemError(message) {
    $('item-error').textContent = message;
    $('item-error').hidden = !message;
  }

  /* Shows the name box when "Create a new …" is chosen. */
  function itemChoice() {
    const isNew = $('existing').value === 'new';
    $('new-name').hidden = !isNew;
    $('name').required = isNew;
    $('item-save').disabled = !$('existing').value;
    itemError('');
    if (isNew && $('item-dialog').open) $('name').focus();
  }

  /* Catalog items that can still be added: the project's saved heads (or all
     projects), minus those already in the draft. */
  function addable(project) {
    const existingProject = project && catalog.find(p => p.id === project.id);
    return project ? (existingProject ? existingProject.heads || [] : []) : catalog;
  }

  /* Opens the dialog to add a project (projectKey null) or a head to one. */
  function openItem(projectKey = null) {
    if (locked) return;
    addingTo = projectKey;
    const project = draft.projects.find(p => p.key === addingTo);
    const itemType = project ? 'expense head' : 'project';
    const current = project ? project.heads : draft.projects;
    const available = addable(project).filter(item => !current.some(c => c.id > 0 && c.id === item.id));
    const canCreate = project ? config.canCreateHead : config.canCreateProject;
    $('item-title').textContent = 'Add ' + itemType;
    $('item-context').textContent = project
      ? 'Add an expense head under ' + project.name + ', then enter its budget lines.'
      : 'Choose a project, then add the heads and lines you want to budget.';
    $('existing-label').firstChild.textContent = 'Choose an existing ' + itemType;
    $('name-label').firstChild.textContent = project ? 'Expense head name' : 'Project name';
    $('item-save').textContent = project ? 'Add head' : 'Add project';
    $('existing').innerHTML = `<option value="">Select ${itemType}</option>`
      + available.map(item => `<option value="${esc(item.id)}">${esc(item.name)}</option>`).join('')
      + (canCreate ? `<option value="new">＋ Create a new ${itemType}</option>` : '');
    $('name').value = '';
    itemError('');
    if (!available.length && canCreate) $('existing').value = 'new';
    itemChoice();
    if (!available.length && !canCreate) itemError('There are no more existing ' + (project ? 'heads' : 'projects') + ' to add. Ask an administrator to create one.');
    $('item-dialog').showModal();
    if ($('existing').value === 'new') $('name').focus();
    else $('existing').focus();
  }

  $('existing').addEventListener('change', itemChoice);
  $('add-project').onclick = () => openItem();
  $('item-cancel').onclick = () => $('item-dialog').close();

  $('item-form').addEventListener('submit', event => {
    event.preventDefault();
    if (locked) return;
    const project = draft.projects.find(p => p.key === addingTo);
    const current = project ? project.heads : draft.projects;
    const newItem = $('existing').value === 'new';
    const options = addable(project);
    const existing = options.find(item => item.id === Number($('existing').value));
    if (newItem && !(project ? config.canCreateHead : config.canCreateProject)) return;
    if (!newItem && !existing) {
      itemError('Choose an existing item or create a new one.');
      return;
    }
    const name = newItem ? $('name').value.trim() : existing.name;
    if (!name) {
      itemError('Enter a name.');
      $('name').focus();
      return;
    }
    const normalized = value => value.trim().normalize('NFKC').toLocaleLowerCase();
    if (current.some(item => normalized(item.name) === normalized(name))) {
      itemError('This name is already included in the draft. Expand the existing item to add its lines.');
      return;
    }
    if (newItem && options.some(item => normalized(item.name) === normalized(name))) {
      itemError('This item already exists. Choose it from the existing items above.');
      return;
    }
    const added = {id: newItem ? 0 : existing.id, name, readOnly: false, key: key(), ...(project ? {lines: []} : {heads: []})};
    current.push(added);
    dirty = true;
    $('item-dialog').close();
    render();
    // A newly added item stays collapsed; its summary is focused for keyboard expansion.
    const summary = $('' + (project ? 'head-' : 'project-') + added.key).querySelector('summary');
    reveal(summary);
  });

  /* ------------------------------------------------------------------ */
  /* Step 3: review and save                                             */
  /* ------------------------------------------------------------------ */

  /* Checks the whole draft; on the first problem shows it, focuses the field
     at fault and returns false. */
  function validate() {
    root.querySelectorAll('[aria-invalid]').forEach(input => input.removeAttribute('aria-invalid'));
    if (!draft.projects.length) {
      error('Add a project, an expense head, and a budget line before saving.', $('add-project'));
      return false;
    }
    for (const project of draft.projects) {
      if (!project.heads.length) {
        error('Add at least one expense head to ' + project.name + '.', root.querySelector('[data-add-head="' + project.key + '"]'));
        return false;
      }
      for (const head of project.heads) {
        if (!head.lines.length) {
          error('Add at least one budget line to ' + project.name + ' / ' + head.name + '.', root.querySelector('[data-add-line="' + head.key + '"]'));
          return false;
        }
        for (const line of head.lines) {
          if (!String(line.description || '').trim()) {
            const input = $('description-' + line.key);
            input.setAttribute('aria-invalid', 'true');
            error('Give every budget line a description.', input);
            return false;
          }
          if (readAmount(line.text) === null) {
            const input = $('amount-' + line.key);
            input.setAttribute('aria-invalid', 'true');
            error('Enter an amount of ₹0 or more, with up to two decimal places. Do not use commas or currency symbols. Maximum line amount: ' + money(MAX) + '.', input);
            return false;
          }
        }
      }
    }
    if (total() === null) {
      error('The combined monthly budget is too large. Reduce it to ' + money(MAX) + ' or less.');
      return false;
    }
    error('');
    return true;
  }

  function reviewLineHTML(line) {
    return `<li><span>${esc(line.description.trim())}</span><strong>${money(readAmount(line.text))}</strong></li>`;
  }

  function reviewHeadHTML(head) {
    return '<details class="bp-head"><summary><span class="bp-item-label">'
      + `<b>${esc(head.name)}</b><small>${head.lines.length} budget lines</small></span>`
      + `<output>${money(headTotal(head))}</output></summary>`
      + `<ul class="bp-review-lines">${head.lines.map(reviewLineHTML).join('')}</ul>`
      + '</details>';
  }

  function reviewProjectHTML(project) {
    const lineCount = project.heads.reduce((n, h) => n + h.lines.length, 0);
    return '<details class="bp-project"><summary><span class="bp-item-label">'
      + `<b>${esc(project.name)}</b><small>${project.heads.length} heads · ${lineCount} lines</small></span>`
      + `<output>${money(projectTotal(project))}</output></summary>`
      + project.heads.map(reviewHeadHTML).join('')
      + '</details>';
  }

  /* Fills step 3: month, starting point, every line, the new projects and
     heads the save will create, and the total. */
  function review() {
    $('review-month').textContent = monthLabel(draft.month);
    $('review-source').textContent = draft.sourceMonth ? 'Copied from ' + monthLabel(draft.sourceMonth) : edit ? 'Existing budget' : 'Started blank';
    $('review-groups').innerHTML = draft.projects.map(reviewProjectHTML).join('');
    const additions = draft.projects.flatMap(p => [
      ...(p.id === 0 ? ['Project: ' + p.name] : []),
      ...p.heads.filter(h => h.id === 0).map(h => 'Head: ' + p.name + ' / ' + h.name),
    ]);
    $('additions').hidden = !additions.length;
    $('additions').innerHTML = '<strong>New projects and heads to create with this budget</strong>'
      + `<ul>${additions.map(name => `<li>${esc(name)}</li>`).join('')}</ul>`;
    $('review-total').textContent = money(total());
  }

  $('review').onclick = () => {
    if (!locked && validate()) {
      review();
      step(3);
    }
  };
  root.querySelectorAll('[data-bp-back]').forEach(button => {
    button.onclick = () => {
      error('');
      step(Number(button.dataset.bpBack));
    };
  });

  /* Saving: the draft goes to the server as JSON in a hidden field. */
  $('save-form').addEventListener('submit', event => {
    if (submitting || locked) {
      event.preventDefault();
      return;
    }
    if (!validate()) {
      event.preventDefault();
      step(2);
      return;
    }
    const payload = {
      month: draft.month,
      sourceMonth: draft.sourceMonth || '',
      revision: draft.revision ?? '',
      exists: Boolean(draft.exists),
      locked: Boolean(draft.locked),
      projects: draft.projects.map(p => ({
        id: p.id,
        name: p.name,
        readOnly: Boolean(p.readOnly),
        heads: p.heads.map(h => ({
          id: h.id,
          name: h.name,
          readOnly: Boolean(h.readOnly),
          lines: h.lines.map(l => ({description: String(l.description).trim(), amount: readAmount(l.text)})),
        })),
      })),
    };
    $('save-draft').value = JSON.stringify(payload);
    $('save-mode').value = edit ? 'edit' : 'create';
    submitting = true;
    $('save').disabled = true;
    $('save').textContent = 'Saving budget…';
  });

  window.addEventListener('beforeunload', event => {
    if (dirty && !submitting) {
      event.preventDefault();
      event.returnValue = '';
    }
  });
  // Coming back with the browser's Back button re-enables the save button.
  window.addEventListener('pageshow', () => {
    submitting = false;
    $('save').disabled = locked;
    $('save').textContent = edit ? 'Save budget changes' : 'Create budget';
  });

  /* ------------------------------------------------------------------ */
  /* Start-up                                                            */
  /* ------------------------------------------------------------------ */

  // Wording for editing a saved month, and for a locked (read-only) month.
  if (edit) {
    $('page-title').textContent = 'Edit monthly budget';
    $('intro').textContent = 'Adjust the plan, then review your changes before saving.';
    $('review-step').textContent = '3. Review & save';
    $('title-3').textContent = 'Review before saving';
    $('save').textContent = 'Save budget changes';
    $('back-month').hidden = true;
    signature = mode() + '|' + (draft.sourceMonth || '');
  }
  if (locked) {
    $('page-title').textContent = 'Monthly budget';
    $('intro').textContent = 'This month is locked. The saved plan is available for review.';
    $('status').textContent = 'This month is locked. Budgets cannot be changed unless an authorized user unlocks the month.';
    $('status').hidden = false;
    $('add-project').hidden = true;
    $('review').hidden = true;
    $('back-month').hidden = true;
    $('save-form').hidden = true;
  } else if (config.saved) {
    $('status').textContent = 'Budget saved. Expand projects and heads to inspect your saved lines, or open budget versus actual to track spending.';
    $('status').hidden = false;
  }

  /* A link to /budgets/plan...#head-<id> opens step 2 at that head. */
  function revealLinkedHead() {
    const match = /^#head-([1-9]\d*)$/.exec(window.location.hash);
    if (!match) return;
    const head = heads().find(item => item.id === Number(match[1]));
    if (!head) return;
    step(2);
    const group = $('head-' + head.key);
    group.open = true;
    reveal(group.querySelector('summary'));
  }
  window.addEventListener('hashchange', revealLinkedHead);

  render();
  // Editing, a locked month, a refused save or an existing draft all start at step 2.
  if (edit || locked || root.dataset.serverError === 'true' || draft.projects.length) {
    signature = mode() + '|' + (draft.sourceMonth || '');
    step(2);
    dirty = !locked && root.dataset.serverError === 'true';
  }
  revealLinkedHead();
})();
