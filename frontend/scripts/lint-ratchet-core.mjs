import { execFileSync } from 'node:child_process';
import { relative, sep } from 'node:path';
import { builtinRules } from 'eslint/use-at-your-own-risk';

export const MAINTAINABILITY_THRESHOLDS = Object.freeze({
  complexity: 15,
  'max-depth': 4,
  'max-lines-per-function': 80,
});
export const MAINTAINABILITY_RULES = new Set(Object.keys(MAINTAINABILITY_THRESHOLDS));
const FUNCTIONS = new Set(['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression']);
const SCOPES = new Set(['ClassDeclaration', 'ClassExpression', 'ObjectExpression']);
const SKIP = new Set(['parent', 'loc', 'range', 'tokens', 'comments']);
const OPTIONS = {
  complexity: 0,
  'max-depth': 0,
  'max-lines-per-function': {
    max: 0,
    skipBlankLines: true,
    skipComments: true,
    IIFEs: true,
  },
};
const children = node => Object.entries(node)
  .filter(([key]) => !SKIP.has(key))
  .flatMap(([, value]) => Array.isArray(value)
    ? value.filter(item => item?.type)
    : value?.type ? [value] : []);
const propertyName = node => {
  if (node?.type === 'PrivateIdentifier') return `#${node.name}`;
  if (node?.type === 'Identifier') return node.name;
  return node && !node.computed && typeof node.value === 'string' ? node.value : undefined;
};

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
    if (typeof item.reason !== 'string' || !item.reason.trim() || item.reason.startsWith('TODO:')) {
      errors.push(`${item.file} :: ${item.function} ${item.rule} missing reason`);
    }
    if (typeof item.owner !== 'string' || !item.owner.trim() || item.owner.startsWith('TODO:')) {
      errors.push(`${item.file} :: ${item.function} ${item.rule} missing owner`);
    }
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
  for (const [id, value] of actual) {
    const [file, fn, rule] = id.split('\0');
    errors.push(`${file} :: ${fn} ${rule} measured=${value} allowed=base threshold; fix or reviewed exception`);
  }
  return errors;
}

export function createIdentityResolver(ast) {
  const cache = new WeakMap(), identities = new WeakMap(), owners = [], seen = new Map(), duplicates = new Set();
  function identityOf(node) {
    if (cache.has(node)) return cache.get(node);
    const chain = [];
    for (let n = node; n && n.type !== 'Program'; n = n.parent) chain.push(n);
    const id = chain.reverse().map(n => identities.get(n)).filter(Boolean).join('.');
    cache.set(node, id);
    return id;
  }
  function register(node, identity) {
    identities.set(node, identity); cache.set(node, identity);
    if (seen.has(identity)) duplicates.add(identity); else seen.set(identity, node);
    owners.push({ node, identity });
  }
  function visit(node, scope = '', counters = { anonymous: 0, class: 0, object: 0, static: 0, computed: 0 }) {
    if (!node?.type) return;
    const parent = node.parent, qualify = part => scope ? `${scope}.${part}` : part;
    if (SCOPES.has(node.type)) {
      let name = node.id?.name;
      if (!name && parent?.type === 'VariableDeclarator') name = propertyName(parent.id);
      if (!name && parent?.type === 'AssignmentExpression') name = propertyName(parent.left);
      if (!name && parent?.type === 'Property') name = propertyName(parent.key);
      if (!name) name = `${node.type === 'ObjectExpression' ? 'object' : 'class'}#${++counters[node.type === 'ObjectExpression' ? 'object' : 'class']}`;
      const next = qualify(name); identities.set(node, next);
      const nested = { anonymous: 0, class: 0, object: 0, static: 0, computed: 0 };
      for (const child of children(node)) {
        const method = child.type === 'MethodDefinition' || child.type === 'Property';
        if (method && child.value && FUNCTIONS.has(child.value.type)) {
          const methodName = `${child.static ? 'static.' : ''}${propertyName(child.key) ?? `computed#${++nested.computed}`}`;
          const methodId = `${next}.${methodName}`;
          identities.set(child, methodName);
          register(child.value, methodId);
          const methodNested = { anonymous: 0, class: 0, object: 0, static: 0, computed: 0 };
          for (const parameter of child.value.params ?? []) visit(parameter, methodId, methodNested);
          if (child.value.body) visit(child.value.body, methodId, methodNested);
        } else visit(child, next, nested);
      }
      return;
    }
    if (node.type === 'PropertyDefinition') {
      const name = propertyName(node.key) ?? `computed#${++counters.computed}`;
      const segment = `<${node.static ? 'static-' : ''}field:${name}>`, id = qualify(segment);
      identities.set(node, id);
      if (node.value && !FUNCTIONS.has(node.value.type)) register(node.value, id);
      if (node.value) {
        const nested = { anonymous: 0, class: 0, object: 0, static: 0, computed: 0 };
        visit(node.value, id, nested);
      }
      return;
    }
    if (node.type === 'StaticBlock') {
      const segment = `<static-block#${++counters.static}>`, id = qualify(segment);
      register(node, id);
      const nested = { anonymous: 0, class: 0, object: 0, static: 0, computed: 0 };
      for (const child of children(node)) visit(child, id, nested);
      return;
    }
    if (FUNCTIONS.has(node.type)) {
      let name = node.type === 'FunctionDeclaration' ? node.id?.name : undefined;
      if (!name && parent?.type === 'VariableDeclarator') name = propertyName(parent.id);
      if (!name && parent?.type === 'AssignmentExpression') name = propertyName(parent.left);
      if (!name && parent?.type === 'PropertyDefinition') name = propertyName(parent.key) ?? `computed#${++counters.computed}`;
      if (!name && (parent?.type === 'Property' || parent?.type === 'MethodDefinition')) {
        name = `${parent.static ? 'static.' : ''}${propertyName(parent.key) ?? `computed#${++counters.computed}`}`;
      }
      if (!name) name = `anonymous#${++counters.anonymous}`;
      const id = parent?.type === 'PropertyDefinition' ? `${identities.get(parent)}.${name}` : qualify(name);
      register(node, id);
      const nested = { anonymous: 0, class: 0, object: 0, static: 0, computed: 0 };
      for (const parameter of node.params ?? []) visit(parameter, id, nested);
      if (node.body) visit(node.body, id, nested);
      return;
    }
    let nextScope = scope;
    if (node.type === 'VariableDeclarator' && node.id?.type === 'Identifier' && !FUNCTIONS.has(node.init?.type)) nextScope = qualify(node.id.name);
    for (const child of children(node)) visit(child, nextScope, counters);
  }
  visit(ast);
  function ownerOf(node) {
    for (let n = node; n; n = n.parent) {
      if (FUNCTIONS.has(n.type) || n.type === 'StaticBlock') return n;
      if (n.type === 'PropertyDefinition') return n.value && FUNCTIONS.has(n.value.type) ? n.value : n;
    }
    return undefined;
  }
  return { identityOf, ownerOf, owners, duplicateIdentities: [...duplicates] };
}

function coreListener(context, state, name) {
  const proxy = Object.create(context);
  Object.defineProperty(proxy, 'options', { value: [OPTIONS[name]] });
  Object.defineProperty(proxy, 'report', { value: descriptor => {
    const reportNode = name === 'max-lines-per-function'
      && ['MethodDefinition', 'Property'].includes(descriptor.node?.type)
      ? descriptor.node.value
      : descriptor.node;
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
  for (const name of Object.keys(OPTIONS)) {
    for (const [selector, handler] of Object.entries(coreListener(context, state, name))) {
      (combined[selector] ??= []).push(handler);
    }
  }
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
      for (const identity of state.identities.duplicateIdentities) {
        context.report({
          node,
          messageId: 'violation',
          data: { payload: JSON.stringify({ file: state.file, duplicateIdentity: identity, violations: [] }) },
        });
      }
    });
    result.Program = (...args) => (combined.Program ?? []).forEach(handler => handler(...args));
    result['Program:exit'] = () => {
      for (const [id, value] of state.measured) {
        const [file, fn, rule] = id.split('\0');
        if (value <= MAINTAINABILITY_THRESHOLDS[rule]) continue;
        const owner = state.identities.owners.find(item => item.identity === fn || item.identity.endsWith(`.${fn}`))?.node;
        context.report({ node: owner ?? context.sourceCode.ast, message: JSON.stringify({ file, function: fn, violations: [rule], [rule]: value }) });
      }
    };
    return result;
  },
} } };
