// The Roadmap headings own the navigation: domain → feature → task.
(() => {
  const board = document.querySelector('[data-task-browser]');
  if (!board || !board.querySelector('[data-tree-view]')) return;
  const tree = board.querySelector('[data-tree-view]');
  const panorama = board.querySelector('[data-panorama-view]');
  const search = board.querySelector('[data-search]');
  const status = board.querySelector('[data-status]');
  const results = board.querySelector('[data-results]');
  const empty = board.querySelector('[data-no-results]');
  const categoryNav = board.querySelector('.category-nav');
  const normalize = value => value.normalize('NFKC').toLocaleLowerCase();
  const groups = [...tree.querySelectorAll('.category-node')].map((element, index) => ({
    element, index: String(index), label: element.dataset.category,
    disclosure: element.querySelector(':scope > .task-group'),
    summary: element.querySelector(':scope > .task-group > .category-row'), records: [],
  }));
  const groupByElement = new Map(groups.map(group => [group.element, group]));
  for (const group of groups) {
    group.parent = groupByElement.get(group.element.parentElement.closest('.category-node'));
    group.element.dataset.categoryIndex = group.index;
    group.disclosure.open = !group.parent;
  }
  const roots = groups.filter(group => !group.parent);
  const records = [...tree.querySelectorAll('.task-node')].map(element => {
    const group = groupByElement.get(element.closest('.category-node'));
    const record = {
      element, id: element.dataset.taskId, phase: element.dataset.phase,
      title: element.dataset.label, state: element.dataset.statusLabel,
      group, categories: [], parent: element.parentElement.closest('.task-node'),
      select: element.querySelector(':scope > .node-line .node-select'),
      toggle: element.querySelector(':scope > .node-line .branch-toggle'),
      children: element.querySelector(':scope > .node-children'),
      info: element.querySelector(':scope > .node-info'),
    };
    for (let ancestor = group; ancestor; ancestor = ancestor.parent) {
      ancestor.records.push(record); record.categories.push(ancestor);
    }
    record.searchText = normalize(`${record.title} ${record.info.querySelector('.description').textContent} ${record.info.querySelector('.task-acceptance')?.textContent || ''} ${record.categories.map(group => group.label).join(' ')}`);
    return record;
  });
  const byID = new Map(records.map(record => [record.id, record]));
  const expanded = new Set();
  let selected = null, mode = 'tree', category = '', beforeFilter = null;
  let directMatches = new Set();
  const make = (tag, className, text) => {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text !== undefined) element.textContent = text;
    return element;
  };
  const matching = group => group.records.filter(record => directMatches.has(record.id));
  const isUnfinished = record => !['accepted', 'closed'].includes(record.phase);
  const openCategory = group => { for (; group; group = group.parent) group.disclosure.open = true; };

  board.classList.add('enhanced');
  board.querySelectorAll('.board-toolbar,.category-nav,.board-context,.board-footer,.node-line,.category-count').forEach(element => { element.hidden = false; });
  for (const record of records) if (record.children) record.children.hidden = true;
  const nav = board.querySelector('[data-categories]');
  for (const [index, label] of [['', '全部领域'], ...roots.map(group => [group.index, group.label])]) {
    const button = make('button', 'category-button');
    button.dataset.categoryFilter = index;
    button.append(make('span', '', label), make('span', 'nav-count'));
    nav.append(button);
  }

  function syncURL() {
    const url = new URL(location.href);
    for (const [name, value] of [['view', mode === 'tree' ? '' : mode], ['q', search.value], ['state', status.value === 'unfinished' ? '' : status.value], ['group', category]]) {
      if (value) url.searchParams.set(name, value); else url.searchParams.delete(name);
    }
    url.hash = selected && directMatches.has(selected) ? `task-${selected}` : '';
    history.replaceState(null, '', url);
  }

  function syncBranches() {
    for (const record of records) {
      const open = expanded.has(record.id);
      if (record.children) record.children.hidden = !open;
      record.toggle.setAttribute('aria-expanded', String(open));
      record.toggle.setAttribute('aria-label', `${open ? '收起' : '展开'} ${record.title} 的子任务`);
      record.toggle.classList.toggle('rotated', open);
      record.info.open = selected === record.id && !record.element.hidden;
      record.select.setAttribute('aria-expanded', String(record.info.open));
      record.element.classList.toggle('selected', record.info.open);
    }
  }

  function visibleTargets() {
    return [...tree.querySelectorAll('.category-row,.node-select')].filter(element => element.getClientRects().length > 0);
  }
  function focusTarget(target) {
    for (const element of tree.querySelectorAll('.category-row,.node-select')) element.tabIndex = element === target ? 0 : -1;
    if (target?.getClientRects().length) target.focus({ preventScroll: true });
  }
  function keepEntryPoint() {
    const visible = visibleTargets();
    let current = visible.find(element => element === document.activeElement);
    if (!current && tree.contains(document.activeElement) && !document.activeElement.getClientRects().length) {
      const container = document.activeElement.closest('.category-node');
      let group = groupByElement.get(container);
      while (group && !visible.includes(group.summary)) group = group.parent;
      if (group) { current = group.summary; focusTarget(current); }
    }
    current ||= visible.find(element => element.tabIndex === 0) || visible[0];
    for (const element of tree.querySelectorAll('.category-row,.node-select')) element.tabIndex = element === current ? 0 : -1;
  }

  function renderPanorama() {
    panorama.replaceChildren();
    for (const group of roots) {
      const matches = matching(group);
      if (!matches.length) continue;
      const card = make('section', 'panorama-card');
      const heading = make('button', 'panorama-heading');
      heading.dataset.drill = group.index;
      heading.append(make('span', '', group.label), make('span', 'nav-count', `${matches.length} 项`));
      card.append(heading);
      const features = groups.filter(child => child.parent === group && matching(child).length);
      const list = make('ul', features.length ? 'panorama-features' : 'panorama-tasks');
      if (features.length) {
        for (const feature of features) {
          const li = make('li'), button = make('button');
          button.dataset.drill = feature.index;
          button.append(make('span', '', feature.label), make('span', 'nav-count', matching(feature).length));
          li.append(button); list.append(li);
        }
      } else {
        for (const record of matches.slice(0, 4)) {
          const li = make('li'), button = make('button');
          button.dataset.jump = record.id;
          button.append(make('span', 'preview-title', record.title), make('span', 'preview-state', record.state));
          li.append(button); list.append(li);
        }
      }
      card.append(list);
      panorama.append(card);
    }
  }

  function paintFilters(reveal = true) {
    const query = normalize(search.value.trim());
    const active = Boolean(query || status.value !== 'unfinished' || category);
    if (active && !beforeFilter) beforeFilter = { expanded: new Set(expanded), groups: groups.map(group => group.disclosure.open) };
    if (!active && beforeFilter) {
      expanded.clear(); for (const id of beforeFilter.expanded) expanded.add(id);
      groups.forEach((group, index) => { group.disclosure.open = beforeFilter.groups[index]; });
      beforeFilter = null;
    }
    const inScope = records.filter(record =>
      (!query || record.searchText.includes(query)) &&
      (status.value === 'all' || (status.value === 'unfinished' ? isUnfinished(record) : record.phase === status.value))
    );
    directMatches = new Set(inScope.filter(record => !category || record.categories.some(group => group.index === category)).map(record => record.id));
    const visible = new Set(directMatches);
    for (const id of directMatches) {
      const record = byID.get(id);
      if (active && reveal) openCategory(record.group);
      let parent = record.parent;
      while (parent) {
        visible.add(parent.dataset.taskId);
        if (active && reveal) expanded.add(parent.dataset.taskId);
        parent = parent.parentElement.closest('.task-node');
      }
    }
    for (const record of records) {
      record.element.hidden = !visible.has(record.id);
      record.element.classList.toggle('context-node', visible.has(record.id) && !directMatches.has(record.id));
    }
    for (const group of groups) {
      const count = matching(group).length;
      group.element.hidden = !count;
      group.element.querySelector(':scope > .task-group > .category-row .category-count').textContent = `${count} 项`;
    }
    let activeRoot = groups.find(group => group.index === category);
    while (activeRoot?.parent) activeRoot = activeRoot.parent;
    nav.querySelectorAll('[data-category-filter]').forEach(button => {
      const index = button.dataset.categoryFilter;
      const count = inScope.filter(record => !index || record.categories.some(group => group.index === index)).length;
      button.querySelector('.nav-count').textContent = count;
      button.setAttribute('aria-current', String(index === (activeRoot?.index || '')));
      button.disabled = !count;
    });
    const scopeName = status.value === 'unfinished' ? '未完成任务' : '任务';
    results.textContent = `${directMatches.size} 项${scopeName}`;
    board.querySelector('.filter-reset').hidden = !active;
    board.querySelector('[data-empty-title]').textContent = status.value === 'unfinished' ? '未完成任务中没有匹配项' : '没有匹配的任务';
    empty.hidden = directMatches.size !== 0;
    tree.hidden = mode !== 'tree' || !directMatches.size;
    panorama.hidden = mode !== 'panorama' || !directMatches.size;
    syncBranches(); renderPanorama(); keepEntryPoint(); syncURL();
  }

  function switchView(value) {
    mode = value === 'panorama' ? 'panorama' : 'tree';
    board.dataset.mode = mode;
    categoryNav.hidden = mode === 'panorama';
    board.querySelectorAll('[data-view]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.view === mode)));
    board.querySelector('.tree-tools').hidden = mode !== 'tree';
    paintFilters(false);
  }

  function inspect(record, force = false) {
    selected = !force && selected === record.id ? null : record.id;
    if (selected) {
      openCategory(record.group);
      let parent = record.parent;
      while (parent) { expanded.add(parent.dataset.taskId); parent = parent.parentElement.closest('.task-node'); }
    }
    syncBranches(); syncURL();
  }

  board.addEventListener('click', event => {
    const button = event.target.closest('button');
    if (!button) return;
    if (button.matches('[data-view]')) return switchView(button.dataset.view);
    if (button.matches('[data-category-filter]')) { category = button.dataset.categoryFilter; return paintFilters(); }
    if (button.matches('.branch-toggle')) {
      const id = button.closest('.task-node').dataset.taskId;
      if (expanded.has(id)) expanded.delete(id); else expanded.add(id);
      syncBranches(); keepEntryPoint(); return;
    }
    if (button.matches('.node-select')) { const record = byID.get(button.closest('.task-node').dataset.taskId); inspect(record); focusTarget(record.select); return; }
    if (button.matches('[data-jump]')) {
      const record = byID.get(button.dataset.jump); switchView('tree'); inspect(record, true);
      focusTarget(record.select); record.select.scrollIntoView({ block: 'center' }); return;
    }
    if (button.matches('[data-drill]')) {
      category = button.dataset.drill; switchView('tree'); paintFilters();
      const group = groups.find(group => group.index === category);
      focusTarget(group.summary); group.summary.scrollIntoView({ block: 'start' }); return;
    }
    if (button.dataset.action === 'reset') { search.value = ''; status.value = 'unfinished'; category = ''; paintFilters(); search.focus(); return; }
    if (['expand', 'collapse'].includes(button.dataset.action)) {
      const open = button.dataset.action === 'expand';
      for (const group of groups) if (!group.element.hidden) group.disclosure.open = open;
      for (const record of records) if (record.children && !record.element.hidden) { if (open) expanded.add(record.id); else expanded.delete(record.id); }
      syncBranches(); keepEntryPoint();
    }
  });
  search.addEventListener('input', () => paintFilters());
  status.addEventListener('change', () => paintFilters());
  search.addEventListener('keydown', event => { if (event.key === 'Escape') { search.value = ''; paintFilters(); } });
  tree.addEventListener('toggle', () => keepEntryPoint(), true);
  tree.addEventListener('focusin', event => { if (event.target.matches('.node-select,.category-row')) focusTarget(event.target); });
  tree.addEventListener('keydown', event => {
    if (event.key === 'Escape' && selected) {
      const record = byID.get(selected); selected = null;
      syncBranches(); syncURL();
      if (record.select.getClientRects().length) focusTarget(record.select); else keepEntryPoint();
      event.preventDefault(); return;
    }
    if (!event.target.matches('.node-select,.category-row')) return;
    const visible = visibleTargets(), index = visible.indexOf(event.target);
    const node = event.target.closest('.task-node'), record = node && byID.get(node.dataset.taskId);
    const group = groups.find(group => group.summary === event.target);
    let target;
    if (event.key === 'ArrowDown') target = visible[Math.min(index + 1, visible.length - 1)];
    else if (event.key === 'ArrowUp') target = visible[Math.max(index - 1, 0)];
    else if (event.key === 'Home') target = visible[0];
    else if (event.key === 'End') target = visible.at(-1);
    else if (event.key === 'ArrowRight') {
      if (group && !group.disclosure.open) group.disclosure.open = true;
      else if (record?.children && !expanded.has(record.id)) { expanded.add(record.id); syncBranches(); }
      else if (group || record?.children) target = visible[index + 1];
    } else if (event.key === 'ArrowLeft') {
      if (group?.disclosure.open) group.disclosure.open = false;
      else if (group) target = group.parent?.summary;
      else if (record?.children && expanded.has(record.id)) { expanded.delete(record.id); syncBranches(); }
      else target = record?.parent ? byID.get(record.parent.dataset.taskId).select : record?.group.summary;
    } else return;
    event.preventDefault(); if (target) { focusTarget(target); target.scrollIntoView({ block: 'nearest' }); } else keepEntryPoint();
  });

  const url = new URL(location.href);
  search.value = url.searchParams.get('q') || '';
  if ([...status.options].some(option => option.value === url.searchParams.get('state'))) status.value = url.searchParams.get('state');
  if (groups.some(group => group.index === url.searchParams.get('group'))) category = url.searchParams.get('group');
  paintFilters(); switchView(url.searchParams.get('view'));
  const initial = byID.get(url.hash.replace(/^#task-/, ''));
  if (initial) {
    if (!directMatches.has(initial.id)) { status.value = 'all'; search.value = ''; category = ''; paintFilters(); }
    switchView('tree'); inspect(initial, true); initial.select.scrollIntoView({ block: 'center' });
  }
})();
