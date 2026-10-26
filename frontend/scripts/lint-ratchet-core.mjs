import { execFileSync } from 'node:child_process';
import { relative, sep } from 'node:path';
import { builtinRules } from 'eslint/use-at-your-own-risk';

export const MAINTAINABILITY_THRESHOLDS = Object.freeze({ complexity: 15, 'max-depth': 4, 'max-lines-per-function': 80 });
export const MAINTAINABILITY_RULES = new Set(Object.keys(MAINTAINABILITY_THRESHOLDS));
const FUNCTIONS = new Set(['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression']);
const SCOPES = new Set(['ClassDeclaration', 'ClassExpression', 'ObjectExpression']);
const SKIP = new Set(['parent', 'loc', 'range', 'tokens', 'comments']);
const OPTIONS = { complexity: 0, 'max-depth': 0, 'max-lines-per-function': { max: 0, skipBlankLines: true, skipComments: true, IIFEs: true } };
const children = node => Object.entries(node).filter(([key]) => !SKIP.has(key)).flatMap(([, value]) => Array.isArray(value) ? value.filter(child => child?.type) : value?.type ? [value] : []);
const propertyName = node => node?.type === 'PrivateIdentifier' ? `#${node.name}` : node?.type === 'Identifier' ? node.name : node && !node.computed && typeof node.value === 'string' ? node.value : undefined;

export function loadBaseBaseline(ref, cwd) {
  const repo = execFileSync('git', ['rev-parse', '--show-toplevel'], { cwd, encoding: 'utf8' }).trim();
  const commit = execFileSync('git', ['rev-parse', '--verify', `${ref}^{commit}`], { cwd: repo, encoding: 'utf8' }).trim();
  const path = 'frontend/maintainability-baseline.json';
  if (!execFileSync('git', ['ls-tree', '-r', '--name-only', commit, `:(top)${path}`], { cwd: repo, encoding: 'utf8' }).trim()) return null;
  let baseline;
  try { baseline = JSON.parse(execFileSync('git', ['show', `${commit}:${path}`], { cwd: repo, encoding: 'utf8' })); }
  catch (error) { if (error instanceof SyntaxError) throw new Error(`malformed baseline at ${ref}: ${error.message}`, { cause: error }); throw error; }
  if (!Array.isArray(baseline)) throw new Error(`baseline at ${ref} must be a JSON array`);
  return baseline;
}
export const metricKey = (file, fn, rule) => `${file}\0${fn}\0${rule}`;
export function compareAgainstBase(current, base) {
  const previous = new Map(base.map(item => [metricKey(item.file, item.function, item.rule), item]));
  return current.flatMap(item => {
    const old = previous.get(metricKey(item.file, item.function, item.rule));
    if (!old) return [`${item.file} :: ${item.function} ${item.rule} new exception relative to base`];
    return item.allowed > old.allowed ? [`${item.file} :: ${item.function} ${item.rule} allowed raised=${item.allowed} base=${old.allowed}`] : [];
  });
}
export function compareMaintainability(measured, baseline) {
  const errors = [], exceptions = new Map();
  for (const item of baseline) {
    const id = metricKey(item.file, item.function, item.rule);
    if (exceptions.has(id)) { errors.push(`${item.file} :: ${item.function} ${item.rule} duplicate exception`); continue; }
    exceptions.set(id, item);
    if (!MAINTAINABILITY_RULES.has(item.rule)) errors.push(`${item.file} invalid rule=${item.rule}`);
    if (typeof item.reason !== 'string' || !item.reason.trim() || item.reason.startsWith('TODO:')) errors.push(`${item.file} :: ${item.function} ${item.rule} missing reason`);
    if (typeof item.owner !== 'string' || !item.owner.trim() || item.owner.startsWith('TODO:')) errors.push(`${item.file} :: ${item.function} ${item.rule} missing owner`);
    if (!Number.isInteger(item.allowed) || item.allowed < 0) errors.push(`${item.file} :: ${item.function} ${item.rule} invalid allowed value`);
  }
  const actual = new Map(measured);
  for (const [id, item] of exceptions) {
    const value = actual.get(id);
    if (value === undefined) errors.push(`${item.file} :: ${item.function} ${item.rule} stale exception: no violation; remove it`);
    else {
      if (value > item.allowed) errors.push(`${item.file} :: ${item.function} ${item.rule} measured=${value} allowed=${item.allowed}`);
      else if (value < item.allowed) errors.push(`${item.file} :: ${item.function} ${item.rule} stale measured=${value} allowed=${item.allowed}; tighten`);
      actual.delete(id);
    }
  }
  for (const [id, value] of actual) { const [file, fn, rule] = id.split('\0'); errors.push(`${file} :: ${fn} ${rule} measured=${value} allowed=base threshold; fix or reviewed exception`); }
  return errors;
}

export function createIdentityResolver(ast) {
  const identities = new WeakMap(), owners = [], seen = new Map(), duplicates = new Set();
  const fresh = () => ({ anonymous: 0, object: 0, class: 0, key: 0, static: 0, computed: 0 });
  function register(node, id) {
    identities.set(node, id);
    if (seen.has(id)) duplicates.add(id); else seen.set(id, node);
    owners.push({ node, identity: id });
  }
  function visit(node, scope = '', counts = fresh()) {
    if (!node?.type) return;
    const parent = node.parent, qualify = part => scope ? `${scope}.${part}` : part;

    if (node.type === 'Decorator') {
      visit(node.expression, qualify(`decorator#${++counts.key}`), fresh());
      return;
    }
    if (SCOPES.has(node.type)) {
      if (node.superClass) visit(node.superClass, qualify(`heritage#${++counts.key}`), fresh());
      let name = node.id?.name;
      if (!name && parent?.type === 'VariableDeclarator') name = propertyName(parent.id);
      if (!name && parent?.type === 'AssignmentExpression') name = propertyName(parent.left);
      if (!name && parent?.type === 'Property' && parent.kind === 'init') name = propertyName(parent.key);
      if (!name && parent?.type === 'ExportDefaultDeclaration') name = 'default';
      if (!name) name = `${node.type === 'ObjectExpression' ? 'object' : 'class'}#${++counts[node.type === 'ObjectExpression' ? 'object' : 'class']}`;
      const id = qualify(name);
      identities.set(node, id);
      for (const child of children(node)) visit(child, id, counts);
      return;
    }
    if (node.type === 'PropertyDefinition') {
      if (node.computed) visit(node.key, qualify(`key#${++counts.key}`), fresh());
      const key = propertyName(node.key) ?? `computed#${++counts.computed}`;
      const id = qualify(`${node.static ? '<static-field:' : '<field:'}${key}>`);
      identities.set(node, id);
      if (node.value && !FUNCTIONS.has(node.value.type)) register(node.value, id);
      if (node.value) {
        if (FUNCTIONS.has(node.value.type)) visit(node.value, scope, fresh());
        else visit(node.value, id, fresh());
      }
      return;
    }
    if (node.type === 'StaticBlock') {
      const id = qualify(`<static-block#${++counts.static}>`);
      register(node, id);
      for (const child of children(node)) visit(child, id, fresh());
      return;
    }
    if (node.type === 'MethodDefinition' || node.type === 'Property') {
      if (node.computed) visit(node.key, qualify(`key#${++counts.key}`), fresh());
      if (node.value && FUNCTIONS.has(node.value.type)) {
        const key = propertyName(node.key) ?? `computed#${++counts.computed}`;
        const kind = node.kind && node.kind !== 'init' ? `<${node.kind}:${key}>` : key;
        const id = qualify(`${node.static ? 'static.' : ''}${kind}`);
        identities.set(node, id);
        register(node.value, id);
        for (const parameter of node.value.params ?? []) visit(parameter, id, fresh());
        if (node.value.body) visit(node.value.body, id, fresh());
      } else {
        const key = propertyName(node.key) ?? `computed#${++counts.computed}`;
        for (const child of children(node)) if (child !== node.key) visit(child, qualify(key), fresh());
      }
      return;
    }
    if (FUNCTIONS.has(node.type)) {
      let name = node.type === 'FunctionDeclaration' ? node.id?.name : undefined;
      if (!name && parent?.type === 'VariableDeclarator') name = propertyName(parent.id);
      if (!name && parent?.type === 'AssignmentExpression') name = propertyName(parent.left);
      if (!name && parent?.type === 'Property') {
        const key = propertyName(parent.key) ?? `computed#${++counts.computed}`;
        name = parent.kind === 'init' ? `${key}.value` : `<${parent.kind}:${key}>`;
      }
      if (!name && parent?.type === 'PropertyDefinition') name = propertyName(parent.key) ?? `computed#${++counts.computed}`;
      if (!name && parent?.type === 'CallExpression' && parent.callee === node) {
        const receiver = node.parent?.type === 'CallExpression' && node.parent.parent?.type === 'MemberExpression'
          ? propertyName(node.parent.parent.property) : undefined;
        name = receiver ? `${receiver}.anonymous#${++counts.anonymous}` : `anonymous#${++counts.anonymous}`;
      }
      if (!name) name = `anonymous#${++counts.anonymous}`;
      const id = parent?.type === 'PropertyDefinition' ? `${identities.get(parent)}.${name}` : qualify(name);
      register(node, id);
      for (const parameter of node.params ?? []) visit(parameter, id, fresh());
      if (node.body) visit(node.body, id, fresh());
      return;
    }
    if (node.type === 'ExportDefaultDeclaration' && node.declaration
      && (FUNCTIONS.has(node.declaration.type) || SCOPES.has(node.declaration.type))) {
      visit(node.declaration, qualify('default'), fresh());
      return;
    }
    for (const child of children(node)) visit(child, scope, counts);
  }
  visit(ast);
  function ownerOf(node) {
    for (let current = node; current; current = current.parent) {
      if (FUNCTIONS.has(current.type) || current.type === 'StaticBlock') return current;
      if (current.type === 'PropertyDefinition') return current.value && FUNCTIONS.has(current.value.type) ? current.value : current;
    }
    return undefined;
  }
  return { identityOf: node => identities.get(node) ?? '', ownerOf, owners, duplicateIdentities: [...duplicates] };
}

function coreListener(context, state, name) {
  const proxy = Object.create(context);
  Object.defineProperty(proxy, 'options', { value: [OPTIONS[name]] });
  Object.defineProperty(proxy, 'report', { value: descriptor => {
    const reportNode = name === 'max-lines-per-function' && ['MethodDefinition', 'Property'].includes(descriptor.node?.type) ? descriptor.node.value : descriptor.node;
    const owner = state.identities.ownerOf(reportNode), identity = owner && state.identities.identityOf(owner), data = descriptor.data ?? {};
    const value = data.complexity ?? data.depth ?? data.lineCount;
    if (!identity || !Number.isInteger(value)) {
      context.report({ node: descriptor.node, message: JSON.stringify({ attributionError: `Unable to attribute ${name} report to a code-path owner` }) });
      return;
    }
    const key = metricKey(state.file, identity, name);
    state.measured.set(key, Math.max(state.measured.get(key) ?? 0, value));
  } });
  return builtinRules.get(name).create(proxy);
}
function listeners(context, state) {
  const combined = {};
  for (const name of Object.keys(OPTIONS)) for (const [selector, handler] of Object.entries(coreListener(context, state, name))) (combined[selector] ??= []).push(handler);
  return combined;
}
export function measurementsFromResults(results) {
  const measured = new Map(), errors = [];
  for (const result of results) for (const message of result.messages) {
    if (message.ruleId !== 'maintainability/metrics') continue;
    const payload = JSON.parse(message.message);
    if (payload.duplicateIdentity) { errors.push(`${payload.file} duplicate function identity: ${payload.duplicateIdentity}`); continue; }
    if (payload.attributionError) { errors.push(`${result.filePath} ${payload.attributionError}`); continue; }
    for (const rule of payload.violations) {
      const key = metricKey(payload.file, payload.function, rule);
      measured.set(key, Math.max(measured.get(key) ?? 0, payload[rule]));
    }
  }
  return { measured, errors };
}
export const maintainabilityPlugin = { rules: { metrics: {
  meta: { type: 'problem', schema: [], messages: { violation: '{{payload}}' } },
  create(context) {
    const state = { file: '', identities: null, measured: new Map() }, combined = listeners(context, state), result = {};
    for (const [selector, handlers] of Object.entries(combined)) result[selector] = (...args) => handlers.forEach(handler => handler(...args));
    (combined.Program ??= []).push(node => {
      state.file = relative(context.cwd, context.filename).split(sep).join('/');
      state.identities = createIdentityResolver(node);
      for (const identity of state.identities.duplicateIdentities) context.report({ node, messageId: 'violation', data: { payload: JSON.stringify({ file: state.file, duplicateIdentity: identity, violations: [] }) } });
    });
    result.Program = (...args) => (combined.Program ?? []).forEach(handler => handler(...args));
    result['Program:exit'] = () => {
      for (const [id, value] of state.measured) {
        const [file, fn, rule] = id.split('\0');
        if (value <= MAINTAINABILITY_THRESHOLDS[rule]) continue;
        const owner = state.identities.owners.find(item => item.identity === fn)?.node;
        context.report({ node: owner ?? context.sourceCode.ast, message: JSON.stringify({ file, function: fn, violations: [rule], [rule]: value }) });
      }
    };
    return result;
  },
} } };
