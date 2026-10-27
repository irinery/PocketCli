package web

import (
	"encoding/json"
	"strings"
)

const realUnlockJS = `(() => {
  'use strict';
  const form = document.querySelector('#unlock-form');
  if (!form) return;
  form.addEventListener('submit', async event => {
    event.preventDefault();
    const input = document.querySelector('#bootstrap');
    const error = document.querySelector('#unlock-error');
    error.textContent = '';
    try {
      const response = await fetch('/api/v1/session/unlock', {
        method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({code: input.value})
      });
      if (!response.ok) throw new Error('Código inválido ou expirado.');
      location.reload();
    } catch (cause) { input.value = ''; error.textContent = cause.message; }
  });
})();`

func realAppJS(investigationID, snapshotID string) string {
	investigation, _ := json.Marshal(investigationID)
	snapshot, _ := json.Marshal(snapshotID)
	return strings.NewReplacer("__INVESTIGATION_ID__", string(investigation), "__SNAPSHOT_ID__", string(snapshot)).Replace(realAppJSTemplate)
}

const realAppJSTemplate = `(() => {
  'use strict';
  const investigationId = __INVESTIGATION_ID__;
  const requestedSnapshotId = __SNAPSHOT_ID__;
  const $ = selector => document.querySelector(selector);
  const main = $('#main');
  const detail = $('#detail');
  let csrf = '', snapshot = null, snapshots = [], findings = [], queries = [], activeView = 'overview';

  const node = (tag, className, value) => {
    const item = document.createElement(tag);
    if (className) item.className = className;
    if (value !== undefined) item.textContent = String(value);
    return item;
  };
  const button = (label, action, className = '') => {
    const item = node('button', className, label);
    item.type = 'button';
    item.addEventListener('click', action);
    return item;
  };
  const card = title => {
    const item = node('section', 'card');
    item.append(node('h2', 'section-title', title));
    return item;
  };
  const heading = (title, subtitle) => {
    const item = node('div', 'page-heading');
    const text = node('div');
    text.append(node('h1', '', title), node('p', '', subtitle));
    item.append(text);
    return item;
  };
  const field = (label, control) => {
    const item = node('div', 'field');
    if (['INPUT', 'SELECT', 'TEXTAREA'].includes(control.tagName)) control.setAttribute('aria-label', label);
    item.append(node('span', '', label), control);
    return item;
  };
  const table = (labels, rows) => {
    const wrapper = node('div', 'result-wrap'), t = node('table', 'data-table');
    const head = node('tr');
    labels.forEach(label => head.append(node('th', '', label)));
    const body = node('tbody');
    rows.forEach(values => {
      const row = node('tr');
      values.forEach(value => {
        const cell = node('td');
        if (value instanceof Node) cell.append(value);
        else cell.textContent = value === null || value === undefined ? 'Não determinado' : String(value);
        row.append(cell);
      });
      body.append(row);
    });
    t.append(node('thead'));
    t.firstChild.append(head);
    t.append(body);
    wrapper.append(t);
    return wrapper;
  };
  const fail = (where, error) => where.append(node('p', 'callout', error.message));
  const segment = value => encodeURIComponent(value);
  async function api(path, options = {}) {
    const headers = new Headers(options.headers || {});
    if (options.body) headers.set('Content-Type', 'application/json');
    if (!['GET', 'HEAD'].includes((options.method || 'GET').toUpperCase())) headers.set('X-CSRF-Token', csrf);
    const response = await fetch('/api/v1' + path, {...options, headers, credentials: 'same-origin'});
    if (response.status === 204) return null;
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || body.code || 'Falha local');
    return body;
  }
  const write = (path, body, method = 'POST') => api(path, {method, body: JSON.stringify(body)});
  function setView(name) {
    if (!snapshot && name !== 'setup') name = 'setup';
    activeView = name;
    document.querySelectorAll('#nav button').forEach(item => item.classList.toggle('active', item.dataset.view === name));
    detail.hidden = true;
    const views = {setup, overview, map, findings: showFindings, sql, comments, claude, reports};
    Promise.resolve().then(() => (views[name] || overview)()).catch(error => { main.replaceChildren(); fail(main, error); });
  }
  async function chooseSnapshot(id) {
    if (!snapshots.some(item => item.id === id)) throw new Error('Snapshot não pertence à investigação selecionada.');
    snapshot = await api('/snapshots/' + segment(id));
    if (snapshot.investigation_id !== investigationId) throw new Error('Snapshot de outra investigação.');
    const [loadedFindings, loadedQueries] = await Promise.all([
      api('/snapshots/' + segment(id) + '/findings'),
      api('/snapshots/' + segment(id) + '/queries')
    ]);
    findings = loadedFindings || [];
    queries = loadedQueries || [];
    $('#snapshot-label').textContent = 'Snapshot ' + id + ' · ' + (snapshot.published_at || snapshot.status);
  }
  async function init() {
    try {
      csrf = (await api('/session')).csrf_token;
      snapshots = await api('/investigations/' + segment(investigationId) + '/snapshots') || [];
      if (!snapshots.length) {
        $('#snapshot-label').textContent = investigationId;
        setView('setup');
        return;
      }
      const latest = [...snapshots].sort((a, b) => (a.published_at || '').localeCompare(b.published_at || '')).at(-1);
      await chooseSnapshot(requestedSnapshotId || latest.id);
      setView('overview');
    } catch (error) { main.replaceChildren(); fail(main, error); }
  }
  function setup() {
    main.replaceChildren(heading('Configurar investigação', 'Escopo, credenciais e período são revisados antes da coleta.'));
    const existing = card('Snapshots publicados');
    if (!snapshots.length) existing.append(node('p', 'empty', 'Nenhum snapshot publicado.'));
    else snapshots.forEach(item => {
      const row = node('div', 'controls');
      row.append(button(item.id, () => chooseSnapshot(item.id).then(() => setView('overview')).catch(error => fail(existing, error))));
      row.append(node('span', 'muted', (item.published_at || '') + ' · ' + item.status));
      existing.append(row);
    });
    main.append(existing);
    const form = card('Novo plano ou refresh');
    const name = node('input'); name.value = 'Investigação ' + investigationId;
    const scope = node('select');
    [['all_authorized', 'Todos os recursos autorizados'], ['selected', 'Recursos selecionados']].forEach(([value, label]) => {
      const option = node('option', '', label); option.value = value; scope.append(option);
    });
    const repositories = node('textarea'), teams = node('textarea');
    repositories.style.minHeight = teams.style.minHeight = '75px';
    repositories.placeholder = 'owner/repo, um por linha';
    teams.placeholder = 'chave ou ID do team, um por linha';
    const sources = node('div', 'controls');
    const github = node('input'), linear = node('input');
    github.type = linear.type = 'checkbox'; github.checked = true;
    github.style.width = linear.style.width = 'auto';
    const githubLabel = node('label'), linearLabel = node('label');
    githubLabel.append(github, document.createTextNode(' GitHub'));
    linearLabel.append(linear, document.createTextNode(' Linear'));
    sources.append(githubLabel, linearLabel);
    const githubKey = node('input'), linearKey = node('input');
    githubKey.type = linearKey.type = 'password';
    githubKey.autocomplete = linearKey.autocomplete = 'off';
    githubKey.placeholder = 'Opcional se já configurada nesta sessão';
    linearKey.placeholder = 'Opcional se já configurada nesta sessão';
    const period = node('select');
    [['30d', '30 dias'], ['60d', '60 dias'], ['90d', '90 dias'], ['1y', '1 ano'], ['all', 'Todo o histórico'], ['custom', 'Datas personalizadas']].forEach(([value, label]) => {
      const option = node('option', '', label); option.value = value; period.append(option);
    });
    period.value = '90d';
    const from = node('input'), to = node('input'), timezone = node('input');
    from.type = to.type = 'date';
    const updatePeriod = () => { const custom = period.value === 'custom'; from.disabled = to.disabled = !custom; };
    period.onchange = updatePeriod;
    updatePeriod();
    timezone.value = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	const discoveryBudget = node('input');
	discoveryBudget.type = 'number'; discoveryBudget.min = '1'; discoveryBudget.step = '1'; discoveryBudget.value = '20';
    const moduleBox = node('div', 'controls'), moduleChecks = {};
    [['github_issues', 'Issues e PRs'], ['code_history', 'Histórico Git'], ['ci', 'CI'], ['deployments', 'Deployments'], ['runtime_import', 'Runtime importado']].forEach(([value, label]) => {
      const choice = node('label'), check = node('input');
      check.type = 'checkbox'; check.checked = value === 'github_issues' || value === 'code_history';
      check.style.width = 'auto';
      choice.append(check, document.createTextNode(' ' + label));
      moduleChecks[value] = check; moduleBox.append(choice);
    });
    const permissions = node('p', 'muted');
    const updatePermissions = () => {
      const required = [];
      if (github.checked) {
        const githubRead = ['Metadata: read', 'Pull requests: read'];
        if (moduleChecks.github_issues.checked) githubRead.push('Issues: read');
        if (moduleChecks.code_history.checked) githubRead.push('Contents: read');
        if (moduleChecks.ci.checked) githubRead.push('Actions: read', 'Checks: read');
        if (moduleChecks.deployments.checked) githubRead.push('Deployments: read', 'Commit statuses: read quando necessário');
        required.push('GitHub: ' + githubRead.join('; '));
      }
      if (linear.checked) required.push('Linear: Read, sem Write/Admin/Create');
      permissions.textContent = required.join(' | ') + (required.length ? '. ' : '') + 'Informe apenas recursos autorizados.';
    };
    const updateSources = () => {
      repositories.disabled = githubKey.disabled = !github.checked;
      teams.disabled = linearKey.disabled = !linear.checked;
      if (!github.checked) {
        repositories.value = githubKey.value = '';
        ['github_issues', 'code_history', 'ci', 'deployments'].forEach(key => { moduleChecks[key].checked = false; });
      }
      if (!linear.checked) teams.value = linearKey.value = '';
      ['github_issues', 'code_history', 'ci', 'deployments'].forEach(key => { moduleChecks[key].disabled = !github.checked; });
      updatePermissions();
    };
    github.onchange = linear.onchange = updateSources;
    Object.values(moduleChecks).forEach(check => { check.onchange = updatePermissions; });
    updateSources();
    const yaml = node('input'); yaml.type = 'file'; yaml.accept = '.yaml,.yml,text/yaml';
    const feedback = node('section');
    let imported = null, plan = null;
    const parseList = text => text.split(/[\n,]+/).map(value => value.trim()).filter(Boolean);
    const importButton = button('Importar YAML', async () => {
      if (!yaml.files || !yaml.files[0]) return;
      feedback.replaceChildren();
      try {
        const file = yaml.files[0];
        const parsed = await write('/setup/parse', {filename: file.name, content: await file.text()});
        if (parsed.missing_fields && parsed.missing_fields.length) throw new Error('Campos faltando: ' + parsed.missing_fields.join(', '));
        imported = parsed.input;
        name.value = imported.name || name.value;
        scope.value = imported.scope && imported.scope.mode || 'all_authorized';
        repositories.value = (imported.scope && imported.scope.repositories || []).join('\n');
        teams.value = (imported.scope && imported.scope.teams || []).join('\n');
        github.checked = !!imported.github_enabled; linear.checked = !!imported.linear_enabled;
        period.value = imported.period.preset;
        from.value = imported.period.from_date || ''; to.value = imported.period.to_date || '';
        updatePeriod();
        timezone.value = imported.period.timezone || timezone.value;
        Object.keys(moduleChecks).forEach(key => { moduleChecks[key].checked = !!imported.modules[key]; });
        updateSources();
        feedback.append(node('p', 'muted', 'YAML importado. Revise os campos antes de preparar o plano.'));
      } catch (error) { fail(feedback, error); }
    });
    const prepare = button('Descobrir recursos e revisar plano', async () => {
      plan = null; feedback.replaceChildren(node('p', 'loading', 'Descobrindo recursos autorizados…'));
      try {
        for (const [source, control] of [['github', githubKey], ['linear', linearKey]]) {
          if (control.value) {
            const secret = control.value; control.value = '';
            await write('/credentials/' + source, {source, secret, persistence: 'session'});
          }
        }
        const input = {
          ...(imported || {}), investigation_id: investigationId, schema_version: 'investigate.v1',
          name: name.value, scope: {mode: scope.value, repositories: parseList(repositories.value), teams: parseList(teams.value)},
          period: {preset: period.value, from_date: period.value === 'custom' ? from.value : '', to_date: period.value === 'custom' ? to.value : '', timezone: timezone.value},
          github_enabled: github.checked, linear_enabled: linear.checked,
          modules: Object.fromEntries(Object.entries(moduleChecks).map(([key, check]) => [key, check.checked])),
          limits: imported && imported.limits || {}
        };
        if (scope.value === 'all_authorized') { input.scope.repositories = []; input.scope.teams = []; }
        const budget = Number(discoveryBudget.value);
        if (!Number.isSafeInteger(budget) || budget < 1) throw new Error('Orçamento de descoberta deve ser um inteiro positivo.');
        plan = await write('/plans' + (budget === 20 ? '' : '?discovery_budget=' + budget), input);
        feedback.replaceChildren(node('h3', '', 'Plano resolvido'));
        feedback.append(node('p', '', 'Hash de confirmação: ' + plan.hash));
        feedback.append(node('p', '', 'Janela: ' + (plan.window_start || 'Todo o histórico') + ' — ' + plan.window_end));
        feedback.append(node('p', 'muted', 'Custo e duração: a estimar. Limites efetivos estão no plano abaixo.'));
        feedback.append(table(['Fonte', 'Recurso', 'Acesso'], (plan.resources || []).map(item => [item.source, item.name, item.access])));
        (plan.warnings || []).forEach(warning => feedback.append(node('p', 'callout', warning)));
        const summary = node('pre', 'code', JSON.stringify(plan.input, null, 2)); feedback.append(summary);
        const consentLabel = node('label'), consent = node('input'); consent.type = 'checkbox';
        consentLabel.append(consent, document.createTextNode(' Confirmei escopo, período, limites e hash deste plano.'));
        const start = button('Confirmar plano e iniciar coleta', async () => {
          start.disabled = true;
          try {
            const run = await write('/runs', {plan_id: plan.id, confirmed_plan_hash: plan.hash, idempotency_key: crypto.randomUUID()});
            const status = node('p', 'muted', 'Run ' + run.id + ' · ' + run.state);
            const controls = node('div', 'controls');
            const transition = async action => {
              try {
                await write('/runs/' + segment(run.id) + '/' + action, {});
                await poll();
              } catch (error) { fail(feedback, error); }
            };
            controls.append(button('Pausar', () => transition('pause')),
              button('Retomar', () => transition('resume')),
              button('Cancelar', () => transition('cancel')));
            feedback.append(status, controls);
            const poll = async () => {
              const current = await api('/runs/' + segment(run.id));
              status.textContent = 'Run ' + run.id + ' · ' + current.state + ' · ' + current.stage + ' · ' + current.items + ' itens';
              if (current.last_error) fail(feedback, new Error(current.last_error.message));
              if (current.snapshot_id) {
                snapshots = await api('/investigations/' + segment(investigationId) + '/snapshots') || [];
                await chooseSnapshot(current.snapshot_id);
              }
              if (['completed', 'completed_partial', 'report_failed', 'failed', 'cancelled', 'paused'].includes(current.state)) {
                if (current.state !== 'paused') controls.querySelectorAll('button').forEach(item => { item.disabled = true; });
                if (current.snapshot_id) feedback.append(button('Abrir snapshot', () => setView('overview')));
                return;
              }
              setTimeout(() => poll().catch(error => fail(feedback, error)), 1500);
            };
            await poll();
          } catch (error) { fail(feedback, error); start.disabled = false; }
        }, 'primary');
        start.disabled = true; consent.onchange = () => { start.disabled = !consent.checked; };
        feedback.append(consentLabel, start);
      } catch (error) { feedback.replaceChildren(); fail(feedback, error); }
    }, 'primary');
    form.append(field('Nome', name), field('Escopo', scope), field('Repositórios GitHub', repositories), field('Teams Linear', teams),
      field('Fontes', sources), permissions,
      field('Token GitHub', githubKey), field('Chave Linear', linearKey), field('Período', period),
      field('Data inicial', from), field('Data final', to), field('Timezone', timezone), field('Módulos', moduleBox),
	  field('Chamadas de descoberta por fonte', discoveryBudget), node('p', 'muted', '20 por padrão. Aumentar este número aprova mais chamadas externas apenas na descoberta; revise o plano antes da coleta.'),
      field('YAML opcional', yaml), importButton, prepare, feedback);
    main.append(form);
  }
  function overview() {
    if (!snapshot) return setup();
    main.replaceChildren(heading('Visão geral', 'Dados publicados em ' + snapshot.published_at));
    const grid = node('div', 'grid');
    const values = [
      ['Achados', findings.length, 'Regras determinísticas'],
      ['Fontes', new Set((snapshot.coverage || []).map(c => c.source)).size, 'Fontes no snapshot'],
      ['Cobertura', snapshot.status, 'Estado da coleta'],
      ['Janela', (snapshot.window_start || 'Início não informado') + ' — ' + snapshot.window_end, 'Período analisado']
    ];
    values.forEach(([label, value, description]) => {
      const item = node('section', 'card metric-card');
      item.append(node('div', 'metric-label', label), node('strong', '', value), node('small', '', description));
      grid.append(item);
    });
    main.append(grid);
    const coverage = card('Cobertura por fonte');
    coverage.append(table(['Fonte', 'Recurso', 'Coleção', 'Estado', 'Observado', 'Motivo'],
      (snapshot.coverage || []).map(c => [c.source, c.resource_id, c.collection, c.status, c.observed, c.reason || '—'])));
    main.append(coverage);
    const recent = card('Achados');
    recent.append(table(['Severidade', 'Achado', 'Evidências'], findings.slice(0, 10).map(f => [
      f.severity, button(f.title, () => findingDetail(f), 'text-button'), (f.evidence_ids || []).length
    ])));
    main.append(recent);
  }
  async function loadReportRecords() {
    let cursor = '', records = [];
    const seen = new Set();
    do {
      const params = new URLSearchParams({limit: '200'});
      if (cursor) params.set('cursor', cursor);
      const page = await api('/snapshots/' + segment(snapshot.id) + '/report?' + params);
      records.push(...(page.records || []));
      if (page.next_cursor && seen.has(page.next_cursor)) throw new Error('Paginação do relatório repetiu cursor.');
      cursor = page.next_cursor || '';
      if (cursor) seen.add(cursor);
    } while (cursor);
    return records;
  }
  async function map() {
    main.replaceChildren(heading('Mapa do produto', 'Componentes e vínculos observados no snapshot.'));
    main.append(node('p', 'loading', 'Carregando registros paginados…'));
    const records = await loadReportRecords();
    main.querySelector('.loading').remove();
    const components = records.filter(r => r.component).map(r => r.component);
    const links = records.filter(r => r.link).map(r => r.link);
    const metrics = records.filter(r => r.metric).map(r => r.metric);
    const componentCard = card('Componentes · ' + components.length);
    const componentFilter = node('input'); componentFilter.placeholder = 'Filtrar componentes';
    const componentResults = node('div');
    const renderComponents = () => {
      const term = componentFilter.value.toLocaleLowerCase();
      componentResults.replaceChildren(table(['Nome', 'Repositório', 'Caminho', 'Implementação', 'Deploy', 'Cobertura'],
        components.filter(c => [c.name, c.repository, c.path, c.coverage].some(value => String(value || '').toLocaleLowerCase().includes(term)))
          .map(c => [c.name, c.repository, c.path, c.implementation_status, c.deployment_status, c.coverage])));
    };
    componentFilter.oninput = renderComponents; renderComponents();
    componentCard.append(field('Filtro', componentFilter), componentResults);
    const linkCard = card('Vínculos · ' + links.length);
    linkCard.append(table(['Origem', 'Relação', 'Destino', 'Estado', 'Evidência'],
      links.map(l => [l.source_id, l.relation, l.target_id, l.status, l.evidence_id])));
    const metricCard = card('Métricas · ' + metrics.length);
    metricCard.append(table(['Nome', 'Valor', 'Unidade', 'Amostra', 'Definição'],
      metrics.map(m => [m.name, m.value && m.value.value, m.unit, m.sample_size, m.definition])));
    main.append(componentCard, linkCard, metricCard);
  }
  function showFindings() {
    main.replaceChildren(heading('Achados', findings.length + ' achados no snapshot.'));
    const filter = node('input'); filter.placeholder = 'Filtrar achados';
    const severity = node('select');
    ['Todas', 'high', 'medium', 'low', 'info'].forEach(value => {
      const option = node('option', '', value); option.value = value === 'Todas' ? '' : value; severity.append(option);
    });
    const controls = node('div', 'controls'), results = node('section');
    controls.append(field('Texto', filter), field('Severidade', severity));
    const render = () => {
      results.replaceChildren();
      const term = filter.value.toLocaleLowerCase();
      const visible = findings.filter(f => (!severity.value || f.severity === severity.value) &&
        [f.title, f.deterministic_text, f.subject_id].some(value => String(value || '').toLocaleLowerCase().includes(term)));
      if (!visible.length) results.append(node('p', 'empty', 'Nenhum achado para este filtro.'));
      visible.forEach(finding => {
        const box = card(finding.title);
        box.append(node('p', '', finding.deterministic_text));
        box.append(node('p', 'muted', finding.severity + ' · ' + finding.evidence_status + ' · ' + (finding.evidence_ids || []).length + ' evidências'));
        box.append(button('Ver evidências', () => findingDetail(finding)));
        results.append(box);
      });
    };
    filter.oninput = render; severity.onchange = render; render();
    main.append(controls, results);
  }
  async function findingDetail(finding) {
    detail.hidden = false;
    detail.replaceChildren(button('Fechar', () => { detail.hidden = true; }, 'close'), node('h2', '', finding.title));
    detail.append(node('p', '', finding.deterministic_text));
    (finding.limitations || []).forEach(item => detail.append(node('p', 'callout', item)));
    for (const id of finding.evidence_ids || []) {
      try {
        const evidence = await api('/snapshots/' + segment(snapshot.id) + '/evidence/' + segment(id));
        const box = card(id + ' · ' + evidence.source);
        box.append(node('p', 'code', evidence.locator), node('p', '', evidence.preview), node('p', 'muted', evidence.observed_at + ' · SHA-256 ' + evidence.sha256));
        detail.append(box);
      } catch (error) { fail(detail, error); }
    }
  }
  function sql() {
    main.replaceChildren(heading('SQL', 'Consulta somente leitura no snapshot selecionado.'));
    const box = card('Consulta');
    const select = node('select');
    select.append(node('option', '', 'Consulta manual'));
    queries.forEach((query, index) => {
      const option = node('option', '', query.title);
      option.value = String(index + 1);
      select.append(option);
    });
    const editor = node('textarea');
    const manualSQL = 'SELECT id, window_start, window_end FROM v_snapshot';
    editor.value = manualSQL;
    const parameters = node('textarea');
    parameters.style.minHeight = '75px';
    parameters.value = '[]';
    const guidance = node('p', 'callout');
    guidance.hidden = true;
    const target = node('input');
    target.placeholder = 'ID da issue, PR ou componente';
    const targetField = field('Alvo da timeline', target);
    targetField.hidden = true;
    targetField.style.display = 'none';
    select.onchange = () => {
      const query = queries[Number(select.value) - 1];
      if (query) {
        editor.value = query.sql;
        parameters.value = JSON.stringify(query.parameters || [], null, 2);
      } else {
        editor.value = manualSQL;
        parameters.value = '[]';
      }
      guidance.textContent = query && query.description || '';
      guidance.hidden = !guidance.textContent;
      target.value = '';
      targetField.hidden = !query || query.id !== 'Q08';
      targetField.style.display = targetField.hidden ? 'none' : '';
    };
    const result = node('section');
    const run = button('Executar consulta', async () => {
      run.disabled = true;
      result.replaceChildren(node('p', 'loading', 'Consultando…'));
      try {
        const bound = JSON.parse(parameters.value);
        const selected = queries[Number(select.value) - 1];
        if (selected && selected.id === 'Q08') {
          const id = target.value.trim();
          if (!id) throw new Error('Informe o ID do alvo para consultar a timeline.');
          const subject = bound.find(item => item.name === 'subject_id');
          if (!subject) throw new Error('Parâmetro subject_id ausente.');
          subject.value = {kind: 'text', value: id};
        }
        const rows = await write('/snapshots/' + segment(snapshot.id) + '/query', {
          snapshot_id: snapshot.id, sql: editor.value, parameters: bound, max_rows: 200, timeout_ms: 5000
        });
        result.replaceChildren(node('p', 'muted', rows.returned_rows + ' linhas · ' + rows.elapsed_ms + ' ms' + (rows.truncated ? ' · truncado' : '')));
        result.append(table((rows.columns || []).map(c => c.name), (rows.rows || []).map(row => row.map(c => c.kind === 'null' ? null : c.value))));
        (rows.warnings || []).forEach(warning => result.append(node('p', 'callout', warning)));
      } catch (error) { result.replaceChildren(); fail(result, error); }
      finally { run.disabled = false; }
    }, 'primary');
    box.append(field('Consultas sugeridas', select), guidance, targetField, field('SQL', editor), field('Parâmetros JSON', parameters), run);
    main.append(box, result);
  }
  async function comments() {
    main.replaceChildren(heading('Comentários', 'Anotações locais do snapshot.'));
    const form = card('Novo comentário');
    const targetType = node('select');
    [['snapshot', 'Snapshot'], ['finding', 'Achado'], ['component', 'Componente']].forEach(([value, label]) => {
      const option = node('option', '', label); option.value = value; targetType.append(option);
    });
    const targetId = node('input'); targetId.value = snapshot.id;
    targetType.onchange = () => { targetId.value = targetType.value === 'snapshot' ? snapshot.id : ''; };
    const operator = node('input'); operator.value = 'Operador';
    const body = node('textarea'); body.style.minHeight = '100px';
    const save = button('Salvar comentário', async () => {
      try {
        await write('/annotations', {investigation_id: investigationId, snapshot_id: snapshot.id,
          target_type: targetType.value, target_id: targetId.value, operator_label: operator.value, body: body.value});
        setView('comments');
      } catch (error) { fail(form, error); }
    }, 'primary');
    form.append(field('Tipo de alvo', targetType), field('ID do alvo', targetId), field('Operador', operator), field('Comentário', body), save);
    main.append(form);
    const list = card('Histórico');
    let cursor = '';
    const seen = new Set();
    do {
      const params = new URLSearchParams({snapshot_id: snapshot.id, limit: '100'});
      if (cursor) params.set('cursor', cursor);
      const page = await api('/annotations?' + params);
      (page.items || []).forEach(item => {
        const row = node('article', 'card');
        row.append(node('h3', '', item.target_type + ' · ' + item.target_id), node('p', '', item.body),
          node('p', 'muted', item.operator_label + ' · revisão ' + item.revision + ' · ' + item.status));
        if (item.status === 'open') {
          row.append(button('Editar', async () => {
            const value = window.prompt('Editar comentário', item.body);
            if (value === null) return;
            try { await write('/annotations/' + segment(item.id), {expected_revision: item.revision, body: value}, 'PATCH'); setView('comments'); }
            catch (error) { fail(row, error); }
          }));
          row.append(button('Resolver', async () => {
            try { await write('/annotations/' + segment(item.id) + '/resolve', {expected_revision: item.revision}); setView('comments'); }
            catch (error) { fail(row, error); }
          }));
        }
        list.append(row);
      });
      if (page.next_cursor && seen.has(page.next_cursor)) throw new Error('Paginação dos comentários repetiu cursor.');
      cursor = page.next_cursor || '';
      if (cursor) seen.add(cursor);
    } while (cursor);
    if (list.childElementCount === 1) list.append(node('p', 'empty', 'Nenhum comentário.'));
    main.append(list);
  }
  async function claude() {
    main.replaceChildren(heading('Claude', 'Redação opcional dos achados selecionados.'));
    const credential = card('Credencial da sessão');
    const key = node('input'); key.type = 'password'; key.autocomplete = 'off';
    credential.append(field('Chave Anthropic', key), button('Guardar nesta sessão', async () => {
      try { await write('/credentials/claude', {source: 'claude', secret: key.value, persistence: 'session'}); key.value = ''; credential.append(node('p', 'muted', 'Chave guardada nesta sessão.')); }
      catch (error) { key.value = ''; fail(credential, error); }
    }), button('Remover chave', async () => {
      try { await api('/credentials/claude', {method: 'DELETE'}); credential.append(node('p', 'muted', 'Chave removida.')); }
      catch (error) { fail(credential, error); }
    }));
    main.append(credential);
    const box = card('Criar rascunho');
    const model = node('select');
    const loadModels = button('Validar chave e listar modelos', async () => {
      try {
        const models = await api('/editorial/models');
        model.replaceChildren();
        models.forEach(item => { const option = node('option', '', item.display_name || item.id); option.value = item.id; model.append(option); });
      } catch (error) { fail(box, error); }
    });
    const choices = node('div', 'controls');
    findings.forEach(finding => {
      const label = node('label'), check = node('input');
      check.type = 'checkbox'; check.value = finding.id; check.checked = true;
      label.append(check, document.createTextNode(' ' + finding.title)); choices.append(label);
    });
    const purpose = node('select'), tone = node('select');
    [['clarity', 'Clareza'], ['executive_summary', 'Resumo executivo'], ['technical_summary', 'Resumo técnico']].forEach(([value, label]) => {
      const option = node('option', '', label); option.value = value; purpose.append(option);
    });
    [['neutral', 'Neutro'], ['direct', 'Direto']].forEach(([value, label]) => {
      const option = node('option', '', label); option.value = value; tone.append(option);
    });
    const previewBox = node('section');
    const preview = button('Ver payload antes do envio', async () => {
      previewBox.replaceChildren();
      try {
        const ids = [...choices.querySelectorAll('input:checked')].map(item => item.value);
        if (!ids.length) throw new Error('Selecione um achado.');
        const result = await write('/editorial/preview', {snapshot_id: snapshot.id, finding_ids: ids,
          annotation_ids: [], purpose: purpose.value, tone: tone.value, model_id: model.value});
        previewBox.append(node('p', 'muted', result.byte_count + ' bytes · hash ' + result.payload_hash));
        const payload = node('textarea'); payload.readOnly = true; payload.value = result.sanitized_payload;
        const label = node('label'), consent = node('input'); consent.type = 'checkbox';
        label.append(consent, document.createTextNode(' Autorizo o envio exato deste payload à Anthropic.'));
        const send = button('Enviar e gerar rascunho', async () => {
          send.disabled = true;
          try { const draft = await write('/editorial/generate', {preview_id: result.id, approved_payload_hash: result.payload_hash}); renderDraft(previewBox, draft); }
          catch (error) { fail(previewBox, error); }
        }, 'primary');
        send.disabled = true; consent.onchange = () => { send.disabled = !consent.checked; };
        previewBox.append(payload, label, send);
      } catch (error) { fail(previewBox, error); }
    }, 'primary');
    box.append(loadModels, field('Modelo', model), choices, field('Objetivo', purpose), field('Tom', tone), preview, previewBox);
    main.append(box);
    const drafts = await api('/editorial/drafts?snapshot_id=' + segment(snapshot.id));
    (drafts || []).forEach(draft => renderDraft(main, draft));
  }
  function renderDraft(parent, draft) {
    const box = card('Rascunho · ' + draft.status);
    (draft.sections || []).forEach(section => box.append(node('h3', '', section.finding_id), node('p', '', section.improved_text),
      node('p', 'muted', 'Evidências: ' + (section.evidence_ids || []).join(', '))));
    if (draft.status === 'pending_review') ['accept', 'reject'].forEach(action => box.append(button(action === 'accept' ? 'Aceitar' : 'Rejeitar', async () => {
      try { await write('/editorial/' + segment(draft.id) + '/' + action, {expected_revision: draft.revision}); setView('claude'); }
      catch (error) { fail(box, error); }
    })));
    parent.append(box);
  }
  async function reports() {
    main.replaceChildren(heading('Relatórios', 'PDF completo do snapshot publicado.'));
    const box = card('Gerar revisão');
    const label = node('label'), accepted = node('input'); accepted.type = 'checkbox';
    label.append(accepted, document.createTextNode(' Incluir redações editoriais aceitas'));
    const generate = button('Gerar PDF', async () => {
      generate.disabled = true;
      try {
        await write('/reports', {snapshot_id: snapshot.id, annotation_revision: 0, include_approved_editorial: accepted.checked});
        setView('reports');
      } catch (error) { fail(box, error); }
      finally { generate.disabled = false; }
    }, 'primary');
    box.append(label, generate); main.append(box);
    const history = card('Revisões');
    const artifacts = await api('/reports?snapshot_id=' + segment(snapshot.id));
    (artifacts || []).forEach(artifact => {
      const link = node('a', '', 'Revisão ' + artifact.revision + ' · ' + artifact.page_count + ' páginas · baixar PDF');
      link.href = '/api/v1/reports/' + segment(artifact.id) + '/download';
      history.append(link, node('p', 'code', artifact.sha256));
    });
    if (!artifacts || !artifacts.length) history.append(node('p', 'empty', 'Nenhum relatório gerado.'));
    main.append(history);
  }
  if (main) {
    $('#nav').addEventListener('click', event => {
      const button = event.target.closest('button[data-view]');
      if (button) setView(button.dataset.view);
    });
    $('#logout').addEventListener('click', async () => { try { await api('/session', {method: 'DELETE'}); } finally { location.reload(); } });
    init();
  }
})();`
