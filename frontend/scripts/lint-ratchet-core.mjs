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
  'max-lines-per-function': { max: 0, skipBlankLines: true, skipComments: true, IIFEs: true },
};

function children(node) {
  const result = [];
  for (const [key, value] of Object.entries(node)) {
    if (SKIP.has(key)) continue;
    if (Array.isArray(value)) result.push(...value.filter(child => child?.type));
    else if (value?.type) result.push(value);
  }
  return result;
}

function propertyName(node) {
  if (node?.type === 'PrivateIdentifier') return `#${node.name}`;
  if (node?.type === 'Identifier') return node.name;
  if (node && !node.computed && typeof node.value === 'string') return node.value;
  return undefined;
}

function git(args, cwd) {
  return execFileSync('git', args, { cwd, encoding: 'utf8' });
}

export function loadBaseBaseline(ref, cwd) {
  const repo = git(['rev-parse', '--show-toplevel'], cwd).trim();
  const commit = git(['rev-parse', '--verify', `${ref}^{commit}`], repo).trim();
  const path = 'frontend/maintainability-baseline.json';
  if (!git(['ls-tree', '-r', '--name-only', commit, `:(top)${path}`], repo).trim()) return null;
  let baseline;
  try {
    baseline = JSON.parse(git(['show', `${commit}:${path}`], repo));
  } catch (error) {
    if (error instanceof SyntaxError) {
      throw new Error(`malformed baseline at ${ref}: ${error.message}`, { cause: error });
    }
    throw error;
  }
  if (!Array.isArray(baseline)) throw new Error(`baseline at ${ref} must be a JSON array`);
  return baseline;
}

export const metricKey = (file, fn, rule) => `${file}\0${fn}\0${rule}`;
const label = item => `${item.file} :: ${item.function} ${item.rule}`;
const blank = value => typeof value !== 'string' || !value.trim() || value.startsWith('TODO:');

export function compareAgainstBase(current, base) {
  const previous = new Map(base.map(item => [metricKey(item.file, item.function, item.rule), item]));
  return current.flatMap(item => {
    const old = previous.get(metricKey(item.file, item.function, item.rule));
    if (!old) return [`${label(item)} new exception relative to base`];
    if (item.allowed > old.allowed) return [`${label(item)} allowed raised=${item.allowed} base=${old.allowed}`];
    return [];
  });
}

function validateExceptions(baseline, errors) {
  const exceptions = new Map();
  for (const item of baseline) {
    const id = metricKey(item.file, item.function, item.rule);
    if (exceptions.has(id)) {
      errors.push(`${label(item)} duplicate exception`);
      continue;
    }
    exceptions.set(id, item);
    if (!MAINTAINABILITY_RULES.has(item.rule)) errors.push(`${item.file} invalid rule=${item.rule}`);
    if (blank(item.reason)) errors.push(`${label(item)} missing reason`);
    if (blank(item.owner)) errors.push(`${label(item)} missing owner`);
    if (!Number.isInteger(item.allowed) || item.allowed < 0) errors.push(`${label(item)} invalid allowed value`);
  }
  return exceptions;
}

export function compareMaintainability(measured, baseline) {
  const errors = [];
  const exceptions = validateExceptions(baseline, errors);
  const actual = new Map(measured);
  for (const [id, item] of exceptions) {
    const value = actual.get(id);
    if (value === undefined) {
      errors.push(`${label(item)} stale exception: no violation; remove it`);
      continue;
    }
    if (value > item.allowed) errors.push(`${label(item)} measured=${value} allowed=${item.allowed}`);
    else if (value < item.allowed) {
      errors.push(`${label(item)} stale measured=${value} allowed=${item.allowed}; tighten`);
    }
    actual.delete(id);
  }
  for (const [id, value] of actual) {
    const [file, fn, rule] = id.split('\0');
    errors.push(`${file} :: ${fn} ${rule} measured=${value} allowed=base threshold; fix or reviewed exception`);
  }
  return errors;
}

const qualified = (scope, part) => (scope ? `${scope}.${part}` : part);

// Assigns every function-like code-path owner a structural identity. Ordinals are
// keyed by the lexical identity prefix, so all siblings under one owner (parameters,
// body statements, static-block statements, decorators) share one sequence.
export function createIdentityResolver(ast) {
  const identities = new WeakMap();
  const owners = [];
  const seen = new Map();
  const duplicates = new Set();
  const ordinals = new Map();

  function next(scope, kind) {
    const slot = `${scope}\0${kind}`;
    const value = (ordinals.get(slot) ?? 0) + 1;
    ordinals.set(slot, value);
    return value;
  }
  function register(node, id) {
    identities.set(node, id);
    if (seen.has(id)) duplicates.add(id);
    else seen.set(id, node);
    owners.push({ node, identity: id });
  }
  function enterFunction(fn, id) {
    register(fn, id);
    for (const parameter of fn.params ?? []) visit(parameter, id);
    if (fn.body) visit(fn.body, id);
  }
  function visitDecorators(node, id) {
    for (const decorator of node.decorators ?? []) {
      visit(decorator.expression, `${id}.decorator#${next(id, 'decorator')}`);
    }
  }
  function memberKey(node, scope) {
    if (node.computed) visit(node.key, qualified(scope, `key#${next(scope, 'key')}`));
    return propertyName(node.key) ?? `computed#${next(scope, 'computed')}`;
  }
  function visitField(node, scope) {
    const key = memberKey(node, scope);
    const id = qualified(scope, `${node.static ? '<static-field:' : '<field:'}${key}>`);
    identities.set(node, id);
    visitDecorators(node, id);
    if (!node.value) return;
    if (FUNCTIONS.has(node.value.type)) {
      enterFunction(node.value, `${id}.${key}`);
      return;
    }
    register(node.value, id);
    visit(node.value, id);
  }
  function visitMember(node, scope) {
    const key = memberKey(node, scope);
    const segment = node.kind && node.kind !== 'init' ? `<${node.kind}:${key}>` : key;
    const id = qualified(scope, `${node.static ? 'static.' : ''}${segment}`);
    identities.set(node, id);
    visitDecorators(node, id);
    if (node.value && FUNCTIONS.has(node.value.type)) {
      enterFunction(node.value, id);
      return;
    }
    for (const child of children(node)) {
      if (child !== node.key && child.type !== 'Decorator') visit(child, id);
    }
  }
  function visitScope(node, scope) {
    const parent = node.parent;
    if (node.superClass) visit(node.superClass, qualified(scope, `heritage#${next(scope, 'heritage')}`));
    let name = node.id?.name;
    if (!name && parent?.type === 'VariableDeclarator') name = propertyName(parent.id);
    if (!name && parent?.type === 'AssignmentExpression') name = propertyName(parent.left);
    if (!name && parent?.type === 'Property' && parent.kind === 'init') name = propertyName(parent.key);
    if (!name && parent?.type === 'ExportDefaultDeclaration') name = 'default';
    if (!name) {
      const kind = node.type === 'ObjectExpression' ? 'object' : 'class';
      name = `${kind}#${next(scope, kind)}`;
    }
    const id = qualified(scope, name);
    identities.set(node, id);
    for (const child of children(node)) {
      if (child !== node.superClass) visit(child, id);
    }
  }
  function functionName(node, scope) {
    const parent = node.parent;
    if (node.type === 'FunctionDeclaration' && node.id?.name) return node.id.name;
    if (parent?.type === 'VariableDeclarator' && propertyName(parent.id)) return propertyName(parent.id);
    if (parent?.type === 'AssignmentExpression' && propertyName(parent.left)) return propertyName(parent.left);
    if (parent?.type === 'ExportDefaultDeclaration') return 'default';
    const anonymous = `anonymous#${next(scope, 'anonymous')}`;
    if (parent?.type === 'CallExpression' && parent.callee === node
      && parent.parent?.type === 'MemberExpression') {
      const receiver = propertyName(parent.parent.property);
      if (receiver) return `${receiver}.${anonymous}`;
    }
    return anonymous;
  }
  function visit(node, scope = '') {
    if (!node?.type) return;
    switch (node.type) {
      case 'Decorator':
        visit(node.expression, qualified(scope, `decorator#${next(scope, 'decorator')}`));
        return;
      case 'PropertyDefinition':
        visitField(node, scope);
        return;
      case 'MethodDefinition':
      case 'Property':
        visitMember(node, scope);
        return;
      case 'StaticBlock': {
        const id = qualified(scope, `<static-block#${next(scope, 'static-block')}>`);
        register(node, id);
        for (const child of children(node)) visit(child, id);
        return;
      }
      default:
        break;
    }
    if (SCOPES.has(node.type)) visitScope(node, scope);
    else if (FUNCTIONS.has(node.type)) enterFunction(node, qualified(scope, functionName(node, scope)));
    else for (const child of children(node)) visit(child, scope);
  }
  visit(ast);
  function ownerOf(node) {
    for (let current = node; current; current = current.parent) {
      if (FUNCTIONS.has(current.type) || current.type === 'StaticBlock') return current;
      if (current.type !== 'PropertyDefinition') continue;
      return current.value && FUNCTIONS.has(current.value.type) ? current.value : current;
    }
    return undefined;
  }
  return {
    identityOf: node => identities.get(node) ?? '',
    ownerOf,
    owners,
    duplicateIdentities: [...duplicates],
  };
}

function coreListener(context, state, name) {
  const proxy = Object.create(context);
  Object.defineProperty(proxy, 'options', { value: [OPTIONS[name]] });
  Object.defineProperty(proxy, 'report', { value: descriptor => recordReport(context, state, name, descriptor) });
  return builtinRules.get(name).create(proxy);
}

function recordReport(context, state, name, descriptor) {
  const isMember = ['MethodDefinition', 'Property'].includes(descriptor.node?.type);
  const reportNode = name === 'max-lines-per-function' && isMember ? descriptor.node.value : descriptor.node;
  const owner = state.identities.ownerOf(reportNode);
  const identity = owner && state.identities.identityOf(owner);
  const data = descriptor.data ?? {};
  const value = data.complexity ?? data.depth ?? data.lineCount;
  if (!identity || !Number.isInteger(value)) {
    const attributionError = `Unable to attribute ${name} report to a code-path owner`;
    context.report({ node: descriptor.node, message: JSON.stringify({ attributionError }) });
    return;
  }
  const key = metricKey(state.file, identity, name);
  state.measured.set(key, Math.max(state.measured.get(key) ?? 0, value));
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
  const measured = new Map();
  const errors = [];
  for (const result of results) {
    for (const message of result.messages) {
      if (message.ruleId !== 'maintainability/metrics') continue;
      const payload = JSON.parse(message.message);
      if (payload.duplicateIdentity) {
        errors.push(`${payload.file} duplicate function identity: ${payload.duplicateIdentity}`);
        continue;
      }
      if (payload.attributionError) {
        errors.push(`${result.filePath} ${payload.attributionError}`);
        continue;
      }
      for (const rule of payload.violations) {
        const key = metricKey(payload.file, payload.function, rule);
        measured.set(key, Math.max(measured.get(key) ?? 0, payload[rule]));
      }
    }
  }
  return { measured, errors };
}

function reportDuplicates(context, state, node) {
  for (const identity of state.identities.duplicateIdentities) {
    const payload = JSON.stringify({ file: state.file, duplicateIdentity: identity, violations: [] });
    context.report({ node, messageId: 'violation', data: { payload } });
  }
}

function reportViolations(context, state) {
  for (const [id, value] of state.measured) {
    const [file, fn, rule] = id.split('\0');
    if (value <= MAINTAINABILITY_THRESHOLDS[rule]) continue;
    const owner = state.identities.owners.find(item => item.identity === fn)?.node;
    const message = JSON.stringify({ file, function: fn, violations: [rule], [rule]: value });
    context.report({ node: owner ?? context.sourceCode.ast, message });
  }
}

export const maintainabilityPlugin = { rules: { metrics: {
  meta: { type: 'problem', schema: [], messages: { violation: '{{payload}}' } },
  create(context) {
    const state = { file: '', identities: null, measured: new Map() };
    const combined = listeners(context, state);
    const result = {};
    for (const [selector, handlers] of Object.entries(combined)) {
      result[selector] = (...args) => handlers.forEach(handler => handler(...args));
    }
    (combined.Program ??= []).push(node => {
      state.file = relative(context.cwd, context.filename).split(sep).join('/');
      state.identities = createIdentityResolver(node);
      reportDuplicates(context, state, node);
    });
    result.Program = (...args) => combined.Program.forEach(handler => handler(...args));
    result['Program:exit'] = () => reportViolations(context, state);
    return result;
  },
} } };
