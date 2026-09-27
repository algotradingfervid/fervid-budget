/* Budget planner: local draft only until the single reviewed form submission. */
(() => {
  'use strict';
  const root = document.getElementById('budget-planner');
  if (!root) return;
  const $ = id => document.getElementById('bp-' + id);
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const money = paise => new Intl.NumberFormat('en-IN', {style:'currency', currency:'INR', minimumFractionDigits:2, maximumFractionDigits:2}).format(paise / 100);
  const MAX = 900000000000000;
  let config;
  try { config = JSON.parse(document.getElementById('budget-planner-data').textContent); }
  catch (_) { $('error').hidden = false; $('error').textContent = 'The budget planner could not load. Reload this page to try again.'; $('continue').disabled = true; return; }
  let serial = 0, dirty = false, submitting = false, addingTo = null, signature = '', busy = false;
  const key = () => ++serial;
  const amountText = amount => {
    if (!Number.isSafeInteger(amount) || amount < 0) return String(amount ?? '');
    return Math.floor(amount / 100) + '.' + String(amount % 100).padStart(2, '0');
  };
  const readAmount = text => {
    const value = String(text).trim();
    if (!/^\d+(?:\.\d{1,2})?$/.test(value) || value.length > 20) return null;
    const [whole, fraction = ''] = value.split('.');
    const paise = BigInt(whole) * 100n + BigInt(fraction.padEnd(2, '0'));
    return paise <= BigInt(MAX) ? Number(paise) : null;
  };
  const hydrate = plan => ({...plan, projects:(plan.projects || []).map(p => ({...p, key:key(), heads:(p.heads || []).map(h => ({...h, key:key(), lines:(h.lines || []).map(l => ({...l, key:key(), text:amountText(l.amount)}))}))}))});
  let draft = hydrate(config.draft || {month:'', projects:[]});
  const locked = Boolean(draft.locked), edit = Boolean(config.edit);
  // Persisted groups cannot be dropped by the planner. Removing a draft group
  // only removes it from this unsaved plan, never from the project/head catalog.
  const savedProjectIDs = new Set(config.savedProjectIds || (edit ? draft.projects.filter(p => p.id > 0).map(p => p.id) : []));
  const savedHeadIDs = new Set(config.savedHeadIds || (edit ? draft.projects.flatMap(p => p.heads).filter(h => h.id > 0).map(h => h.id) : []));
  const canRemoveProject = p => !locked && !p.readOnly && (!edit || !savedProjectIDs.has(p.id));
  const canRemoveHead = h => !locked && !h.readOnly && (!edit || !savedHeadIDs.has(h.id));
  const catalog = config.catalog || [], sources = config.sources || [];
  const heads = () => draft.projects.flatMap(p => p.heads);
  const lines = () => heads().flatMap(h => h.lines);
  const isReadOnly = (p, h) => locked || p.readOnly || (h && h.readOnly);
  const sum = values => {
    let total = 0;
    for (const value of values) { if (value === null || !Number.isSafeInteger(value) || value > MAX - total) return null; total += value; }
    return total;
  };
  const headTotal = h => sum(h.lines.map(l => readAmount(l.text)));
  const projectTotal = p => sum(p.heads.map(headTotal));
  const total = () => sum(draft.projects.map(projectTotal));
  const showMoney = value => value === null ? 'Check amounts' : money(value);
  const monthLabel = month => /^\d{4}-(0[1-9]|1[0-2])$/.test(month || '') ? new Intl.DateTimeFormat('en-IN', {month:'long', year:'numeric', timeZone:'UTC'}).format(new Date(month + '-01T12:00:00Z')) : month;
  const mode = () => root.querySelector('input[name="bp-start"]:checked').value;
  function error(message, focus) {
    $('error').textContent = message; $('error').hidden = !message;
    if (message && focus) reveal(focus);
    else if (message) $('error').focus();
  }
  function reveal(element) {
    if (!element) { $('error').focus(); return; }
    // Focusing a summary reveals its ancestors, but does not expand the item itself.
    const ancestor = element.tagName === 'SUMMARY' ? element.parentElement.parentElement : element.parentElement;
    for (let parent = ancestor; parent; parent = parent.parentElement) if (parent.tagName === 'DETAILS') parent.open = true;
    element.focus(); element.scrollIntoView({block:'center', behavior:'instant'});
  }
  function step(number) {
    for (let i = 1; i <= 3; i++) $('step-' + i).hidden = i !== number;
    root.querySelectorAll('.bp-steps li').forEach((li, i) => { if (i === number - 1) li.setAttribute('aria-current', 'step'); else li.removeAttribute('aria-current'); });
    $('title-' + number).focus();
    root.scrollIntoView({block:'start', behavior:'instant'});
  }
  function context() {
    $('context').textContent = monthLabel(draft.month) + ' · ' + (draft.sourceMonth ? 'Copied from ' + monthLabel(draft.sourceMonth) : edit ? 'Editing the saved budget' : 'Starting blank');
    $('grid-link').href = '/grid?month=' + encodeURIComponent(draft.month || $('month').value);
  }
  function updateTotals() {
    draft.projects.forEach(p => { $('project-total-' + p.key).textContent = showMoney(projectTotal(p)); p.heads.forEach(h => { $('head-total-' + h.key).textContent = showMoney(headTotal(h)); }); });
    $('live-total').textContent = showMoney(total());
    $('count').textContent = !draft.projects.length ? 'No items added yet' : heads().filter(h => headTotal(h) > 0).length + ' of ' + heads().length + ' expense heads have a budget · ' + draft.projects.length + ' projects';
  }
  function render() {
    const open = new Set(Array.from($('groups').querySelectorAll('details[open]'), d => d.id));
    $('outline-controls').hidden = !draft.projects.length;
    $('groups').innerHTML = draft.projects.length ? draft.projects.map(p => {
      const projectReadOnly = isReadOnly(p);
      return '<details class="bp-project" id="bp-project-' + p.key + '" ' + (open.has('bp-project-' + p.key) ? 'open' : '') + '><summary><span class="bp-item-label"><b>' + esc(p.name) + '</b>' + (p.id === 0 ? '<span class="bp-tag">New project</span>' : '') + (p.readOnly ? '<span class="bp-tag">Retired · read only</span>' : '') + '<small>' + p.heads.length + ' expense heads</small></span><output id="bp-project-total-' + p.key + '" aria-label="Total for project ' + esc(p.name) + '"></output></summary>' + p.heads.map(h => {
        const readOnly = isReadOnly(p, h);
        return '<details class="bp-head" id="bp-head-' + h.key + '" ' + (open.has('bp-head-' + h.key) ? 'open' : '') + '><summary><span class="bp-item-label"><b>' + esc(h.name) + '</b>' + (h.id === 0 ? '<span class="bp-tag">New head</span>' : '') + (h.readOnly ? '<span class="bp-tag">Retired · read only</span>' : '') + '<small>' + h.lines.length + ' budget lines</small></span><output id="bp-head-total-' + h.key + '" aria-label="Total for head ' + esc(h.name) + '"></output></summary>' + h.lines.map((l, index) => '<div class="bp-line"><label for="bp-description-' + l.key + '">Line ' + (index + 1) + ' description<input id="bp-description-' + l.key + '" data-description="' + l.key + '" aria-label="Line ' + (index + 1) + ' description for ' + esc(p.name + ' / ' + h.name) + '" placeholder="What is this budget for?" value="' + esc(l.description) + '" maxlength="240" required ' + (readOnly ? 'disabled' : '') + '></label><label for="bp-amount-' + l.key + '">Amount (₹)<input id="bp-amount-' + l.key + '" data-amount="' + l.key + '" aria-label="Line ' + (index + 1) + ' amount for ' + esc(p.name + ' / ' + h.name) + '" inputmode="decimal" autocomplete="off" value="' + esc(l.text) + '" required ' + (readOnly ? 'disabled' : '') + '></label>' + (!readOnly ? '<button type="button" data-remove="' + l.key + '" aria-label="Remove line ' + (index + 1) + ' from ' + esc(p.name + ' / ' + h.name) + '">Remove</button>' : '') + '</div>').join('') + (!h.lines.length ? '<p class="bp-line-hint">No budget lines yet. Add your first line below.</p>' : '') + (!readOnly ? '<div class="bp-head-actions"><button type="button" class="bp-add-line" data-add-line="' + h.key + '" aria-label="Add line to ' + esc(p.name + ' / ' + h.name) + '">＋ Add line</button>' + (canRemoveHead(h) ? '<button type="button" class="bp-remove-group" data-remove-head="' + h.key + '" aria-label="Remove draft head ' + esc(p.name + ' / ' + h.name) + '">Remove head from draft</button>' : '') + '</div>' : '') + '</details>';
      }).join('') + (!p.heads.length ? '<p class="muted">No heads yet. Add your first expense head below.</p>' : '') + (!projectReadOnly ? '<div class="bp-project-foot"><button type="button" data-add-head="' + p.key + '" aria-label="Add head to ' + esc(p.name) + '">＋ Add expense head</button>' + (canRemoveProject(p) ? '<button type="button" class="bp-remove-group" data-remove-project="' + p.key + '" aria-label="Remove draft project ' + esc(p.name) + '">Remove project from draft</button>' : '') + '</div>' : '') + '</details>';
    }).join('') : '<div class="bp-empty"><strong>Your budget starts here</strong><p class="muted">No projects, heads, or lines yet.<br>Add your first project to begin.</p></div>';
    updateTotals(); context();
  }
  function sourcePreview() {
    $('source').hidden = mode() !== 'copy';
    const source = sources.find(s => s.month === $('source-month').value);
    $('source-preview').textContent = source ? money(source.budget) + ' in the original month. Retired items are excluded from the copy; review its new total in the next step.' : 'No saved budgets are available to copy yet.';
  }
  $('month').value = draft.month;
  $('month').disabled = edit || locked;
  $('source-month').innerHTML = sources.map(s => '<option value="' + esc(s.month) + '">' + esc(monthLabel(s.month)) + '</option>').join('');
  if (draft.sourceMonth && sources.some(s => s.month === draft.sourceMonth)) { $('source-month').value = draft.sourceMonth; $('copy').checked = true; }
  $('copy').disabled = !sources.length;
  root.querySelectorAll('input[name="bp-start"]').forEach(radio => radio.addEventListener('change', sourcePreview));
  $('source-month').addEventListener('change', sourcePreview);
  sourcePreview();
  $('continue').addEventListener('click', async () => {
    if (busy) return;
    const month = $('month').value;
    if (!/^(?!0000)\d{4}-(0[1-9]|1[0-2])$/.test(month)) { error('Choose a valid budget month.', $('month')); return; }
    const sourceMonth = mode() === 'copy' ? $('source-month').value : '';
    if (mode() === 'copy' && !sourceMonth) { error('Choose a source month or start with a blank budget.', $('source-month')); return; }
    if (sourceMonth === month) { error('Choose a different month from the budget you are copying.', $('month')); return; }
    const next = mode() + '|' + sourceMonth;
    if (signature !== next && dirty && !window.confirm('Changing the starting point will discard the project, head, and line changes in this unsaved draft. Continue?')) return;
    error('');
    if (signature !== next) {
      let projects = [];
      if (sourceMonth) {
        busy = true; $('continue').disabled = true; $('continue').textContent = 'Loading budget…';
        $('month').disabled = true; $('source-month').disabled = true;
        root.querySelectorAll('input[name="bp-start"]').forEach(input => { input.disabled = true; });
        try {
          const response = await fetch('/budgets/plan-data?month=' + encodeURIComponent(sourceMonth), {headers:{Accept:'application/json'}});
          if (!response.ok) throw new Error('The source budget could not be loaded. It may no longer be available to your account.');
          const plan = await response.json();
          if (!plan || !Array.isArray(plan.projects)) throw new Error('The source budget returned an unexpected response. Reload this page and try again.');
          projects = plan.projects.filter(p => !p.readOnly).map(p => ({...p, heads:(p.heads || []).filter(h => !h.readOnly)})).filter(p => p.heads.length);
        } catch (problem) { error(problem.message || 'The source budget could not be loaded. Try again.'); return; }
        finally {
          busy = false; $('continue').disabled = false; $('continue').textContent = 'Continue to amounts →';
          $('month').disabled = edit || locked; $('source-month').disabled = false;
          root.querySelectorAll('input[name="bp-start"]').forEach(input => { input.disabled = false; });
          $('copy').disabled = !sources.length;
        }
      }
      draft = hydrate({...draft, projects, sourceMonth, month});
      dirty = projects.length > 0; signature = next;
    }
    if (draft.month !== month) dirty = true;
    draft.month = month; render(); step(2);
  });
  $('expand').onclick = () => $('groups').querySelectorAll('details').forEach(d => { d.open = true; });
  $('collapse').onclick = () => $('groups').querySelectorAll('details').forEach(d => { d.open = false; });
  $('groups').addEventListener('input', event => {
    const target = event.target;
    if (target.dataset.amount) {
      const line = lines().find(l => l.key === Number(target.dataset.amount));
      if (!line) return;
      line.text = target.value; dirty = true;
      if (readAmount(line.text) === null) target.setAttribute('aria-invalid', 'true'); else target.removeAttribute('aria-invalid');
      updateTotals();
    } else if (target.dataset.description) {
      const line = lines().find(l => l.key === Number(target.dataset.description));
      if (!line) return;
      line.description = target.value; dirty = true; target.removeAttribute('aria-invalid');
    }
  });
  $('groups').addEventListener('click', event => {
    const button = event.target.closest('button');
    if (!button || locked) return;
    if (button.dataset.addLine) {
      const head = heads().find(h => h.key === Number(button.dataset.addLine));
      if (!head || head.readOnly) return;
      const line = {key:key(), description:'', text:'0.00'};
      head.lines.push(line); dirty = true; render(); reveal($('description-' + line.key));
    } else if (button.dataset.remove) {
      const id = Number(button.dataset.remove), head = heads().find(h => h.lines.some(l => l.key === id));
      if (!head || head.readOnly) return;
      head.lines = head.lines.filter(l => l.key !== id); dirty = true; render(); reveal(root.querySelector('[data-add-line="' + head.key + '"]'));
    } else if (button.dataset.addHead) openItem(Number(button.dataset.addHead));
    else if (button.dataset.removeHead) {
      const id = Number(button.dataset.removeHead), project = draft.projects.find(p => p.heads.some(h => h.key === id));
      const head = project && project.heads.find(h => h.key === id);
      if (!head || !canRemoveHead(head)) return;
      if (head.lines.length && !window.confirm('Remove ' + head.name + ' and its budget lines from this unsaved draft? The expense head itself will not be deleted.')) return;
      project.heads = project.heads.filter(h => h.key !== id); dirty = true; error(''); render();
      reveal(root.querySelector('[data-add-head="' + project.key + '"]'));
    } else if (button.dataset.removeProject) {
      const id = Number(button.dataset.removeProject), project = draft.projects.find(p => p.key === id);
      if (!project || !canRemoveProject(project)) return;
      if (project.heads.length && !window.confirm('Remove ' + project.name + ' and its heads and budget lines from this unsaved draft? Existing projects and expense heads will not be deleted.')) return;
      draft.projects = draft.projects.filter(p => p.key !== id); dirty = true; error(''); render(); $('add-project').focus();
    }
  });
  function itemError(message) { $('item-error').textContent = message; $('item-error').hidden = !message; }
  function itemChoice() {
    const isNew = $('existing').value === 'new';
    $('new-name').hidden = !isNew; $('name').required = isNew;
    $('item-save').disabled = !$('existing').value;
    itemError('');
    if (isNew && $('item-dialog').open) $('name').focus();
  }
  function openItem(projectKey = null) {
    if (locked) return;
    addingTo = projectKey;
    const project = draft.projects.find(p => p.key === addingTo), itemType = project ? 'expense head' : 'project';
    const existingProject = project && catalog.find(p => p.id === project.id);
    const current = project ? project.heads : draft.projects;
    const available = (project ? (existingProject ? existingProject.heads || [] : []) : catalog).filter(item => !current.some(c => c.id > 0 && c.id === item.id));
    const canCreate = project ? config.canCreateHead : config.canCreateProject;
    $('item-title').textContent = 'Add ' + itemType;
    $('item-context').textContent = project ? 'Add an expense head under ' + project.name + ', then enter its budget lines.' : 'Choose a project, then add the heads and lines you want to budget.';
    $('existing-label').firstChild.textContent = 'Choose an existing ' + itemType;
    $('name-label').firstChild.textContent = project ? 'Expense head name' : 'Project name';
    $('item-save').textContent = project ? 'Add head' : 'Add project';
    $('existing').innerHTML = '<option value="">Select ' + itemType + '</option>' + available.map(item => '<option value="' + item.id + '">' + esc(item.name) + '</option>').join('') + (canCreate ? '<option value="new">＋ Create a new ' + itemType + '</option>' : '');
    $('name').value = ''; itemError('');
    if (!available.length && canCreate) $('existing').value = 'new';
    itemChoice();
    if (!available.length && !canCreate) itemError('There are no more existing ' + (project ? 'heads' : 'projects') + ' to add. Ask an administrator to create one.');
    $('item-dialog').showModal();
    if ($('existing').value === 'new') $('name').focus(); else $('existing').focus();
  }
  $('existing').addEventListener('change', itemChoice);
  $('add-project').onclick = () => openItem();
  $('item-cancel').onclick = () => $('item-dialog').close();
  $('item-form').addEventListener('submit', event => {
    event.preventDefault();
    if (locked) return;
    const project = draft.projects.find(p => p.key === addingTo), current = project ? project.heads : draft.projects;
    const newItem = $('existing').value === 'new';
    const existingProject = project && catalog.find(p => p.id === project.id);
    const options = project ? (existingProject ? existingProject.heads || [] : []) : catalog;
    const existing = options.find(item => item.id === Number($('existing').value));
    if (newItem && !(project ? config.canCreateHead : config.canCreateProject)) return;
    if (!newItem && !existing) { itemError('Choose an existing item or create a new one.'); return; }
    const name = newItem ? $('name').value.trim() : existing.name;
    if (!name) { itemError('Enter a name.'); $('name').focus(); return; }
    const normalized = value => value.trim().normalize('NFKC').toLocaleLowerCase();
    if (current.some(item => normalized(item.name) === normalized(name))) { itemError('This name is already included in the draft. Expand the existing item to add its lines.'); return; }
    if (newItem && options.some(item => normalized(item.name) === normalized(name))) { itemError('This item already exists. Choose it from the existing items above.'); return; }
    const added = {id:newItem ? 0 : existing.id, name, readOnly:false, key:key(), ...(project ? {lines:[]} : {heads:[]})};
    current.push(added); dirty = true; $('item-dialog').close(); render();
    // A newly added item stays collapsed; its summary is focused for keyboard expansion.
    const summary = $('' + (project ? 'head-' : 'project-') + added.key).querySelector('summary');
    reveal(summary);
  });
  function validate() {
    root.querySelectorAll('[aria-invalid]').forEach(input => input.removeAttribute('aria-invalid'));
    if (!draft.projects.length) { error('Add a project, an expense head, and a budget line before saving.', $('add-project')); return false; }
    for (const project of draft.projects) {
      if (!project.heads.length) { error('Add at least one expense head to ' + project.name + '.', root.querySelector('[data-add-head="' + project.key + '"]')); return false; }
      for (const head of project.heads) {
        if (!head.lines.length) { error('Add at least one budget line to ' + project.name + ' / ' + head.name + '.', root.querySelector('[data-add-line="' + head.key + '"]')); return false; }
        for (const line of head.lines) {
          if (!String(line.description || '').trim()) { const input = $('description-' + line.key); input.setAttribute('aria-invalid','true'); error('Give every budget line a description.', input); return false; }
          if (readAmount(line.text) === null) { const input = $('amount-' + line.key); input.setAttribute('aria-invalid','true'); error('Enter an amount of ₹0 or more, with up to two decimal places. Do not use commas or currency symbols. Maximum line amount: ' + money(MAX) + '.', input); return false; }
        }
      }
    }
    if (total() === null) { error('The combined monthly budget is too large. Reduce it to ' + money(MAX) + ' or less.'); return false; }
    error(''); return true;
  }
  function review() {
    $('review-month').textContent = monthLabel(draft.month);
    $('review-source').textContent = draft.sourceMonth ? 'Copied from ' + monthLabel(draft.sourceMonth) : edit ? 'Existing budget' : 'Started blank';
    $('review-groups').innerHTML = draft.projects.map(project => '<details class="bp-project"><summary><span class="bp-item-label"><b>' + esc(project.name) + '</b><small>' + project.heads.length + ' heads · ' + project.heads.reduce((n,h) => n + h.lines.length,0) + ' lines</small></span><output>' + money(projectTotal(project)) + '</output></summary>' + project.heads.map(head => '<details class="bp-head"><summary><span class="bp-item-label"><b>' + esc(head.name) + '</b><small>' + head.lines.length + ' budget lines</small></span><output>' + money(headTotal(head)) + '</output></summary><ul class="bp-review-lines">' + head.lines.map(line => '<li><span>' + esc(line.description.trim()) + '</span><strong>' + money(readAmount(line.text)) + '</strong></li>').join('') + '</ul></details>').join('') + '</details>').join('');
    const additions = draft.projects.flatMap(p => [...(p.id === 0 ? ['Project: ' + p.name] : []), ...p.heads.filter(h => h.id === 0).map(h => 'Head: ' + p.name + ' / ' + h.name)]);
    $('additions').hidden = !additions.length;
    $('additions').innerHTML = '<strong>New projects and heads to create with this budget</strong><ul>' + additions.map(name => '<li>' + esc(name) + '</li>').join('') + '</ul>';
    $('review-total').textContent = money(total());
  }
  $('review').onclick = () => { if (!locked && validate()) { review(); step(3); } };
  root.querySelectorAll('[data-bp-back]').forEach(button => { button.onclick = () => { error(''); step(Number(button.dataset.bpBack)); }; });
  $('save-form').addEventListener('submit', event => {
    if (submitting || locked) { event.preventDefault(); return; }
    if (!validate()) { event.preventDefault(); step(2); return; }
    const payload = {month:draft.month, sourceMonth:draft.sourceMonth || '', revision:draft.revision ?? '', exists:Boolean(draft.exists), locked:Boolean(draft.locked), projects:draft.projects.map(p => ({id:p.id, name:p.name, readOnly:Boolean(p.readOnly), heads:p.heads.map(h => ({id:h.id, name:h.name, readOnly:Boolean(h.readOnly), lines:h.lines.map(l => ({description:String(l.description).trim(), amount:readAmount(l.text)}))}))}))};
    $('save-draft').value = JSON.stringify(payload); $('save-mode').value = edit ? 'edit' : 'create';
    submitting = true; $('save').disabled = true; $('save').textContent = 'Saving budget…';
  });
  window.addEventListener('beforeunload', event => { if (dirty && !submitting) { event.preventDefault(); event.returnValue = ''; } });
  window.addEventListener('pageshow', () => { submitting = false; $('save').disabled = locked; $('save').textContent = edit ? 'Save budget changes' : 'Create budget'; });
  if (edit) {
    $('page-title').textContent = 'Edit monthly budget'; $('intro').textContent = 'Adjust the plan, then review your changes before saving.';
    $('review-step').textContent = '3. Review & save'; $('title-3').textContent = 'Review before saving'; $('save').textContent = 'Save budget changes'; $('back-month').hidden = true;
    signature = mode() + '|' + (draft.sourceMonth || '');
  }
  if (locked) {
    $('page-title').textContent = 'Monthly budget'; $('intro').textContent = 'This month is locked. The saved plan is available for review.';
    $('status').textContent = 'This month is locked. Budgets cannot be changed unless an authorized user unlocks the month.'; $('status').hidden = false;
    $('add-project').hidden = true; $('review').hidden = true; $('back-month').hidden = true; $('save-form').hidden = true;
  } else if (config.saved) {
    $('status').textContent = 'Budget saved. Expand projects and heads to inspect your saved lines, or open budget versus actual to track spending.'; $('status').hidden = false;
  }
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
  if (edit || locked || root.dataset.serverError === 'true' || draft.projects.length) {
    signature = mode() + '|' + (draft.sourceMonth || ''); step(2);
    dirty = !locked && root.dataset.serverError === 'true';
  }
  revealLinkedHead();
})();
