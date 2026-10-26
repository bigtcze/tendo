import { execFileSync } from 'node:child_process';
import { builtinRules } from 'eslint/use-at-your-own-risk';

export const MAINTAINABILITY_THRESHOLDS = Object.freeze({
  complexity: 15,
  'max-depth': 4,
  'max-lines-per-function': 80,
});
export const MAINTAINABILITY_RULES = new Set(Object.keys(MAINTAINABILITY_THRESHOLDS));

export function loadBaseBaseline(ref, cwd) {
  const repoRoot = execFileSync('git', ['rev-parse', '--show-toplevel'], { cwd, encoding: 'utf8' }).trim();
  const commit = execFileSync('git', ['rev-parse', '--verify', `${ref}^{commit}`], { cwd: repoRoot, encoding: 'utf8' }).trim();
  const path = 'frontend/maintainability-baseline.json';
  const treeEntry = execFileSync('git', ['ls-tree', '-r', '--name-only', commit, `:(top)${path}`], { cwd: repoRoot, encoding: 'utf8' });
  if (!treeEntry.trim()) return null;
  const content = execFileSync('git', ['show', `${commit}:${path}`], { cwd: repoRoot, encoding: 'utf8' });
  let baseline;
  try {
    baseline = JSON.parse(content);
  } catch (error) {
    throw new Error(`malformed baseline at ${ref}: ${error.message}`, { cause: error });
  }
  if (!Array.isArray(baseline)) throw new Error(`baseline at ${ref} must be a JSON array`);
  return baseline;
}

export function metricKey(file, fn, rule) {
  return `${file}\0${fn}\0${rule}`;
}

export function compareAgainstBase(current, base) {
  const previous = new Map(
    base.map(entry => [metricKey(entry.file, entry.function, entry.rule), entry]),
  );
  const errors = [];
  for (const entry of current) {
    const old = previous.get(metricKey(entry.file, entry.function, entry.rule));
    if (!old) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} new exception relative to base`);
    } else if (entry.allowed > old.allowed) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} allowed raised=${entry.allowed} base=${old.allowed}`);
    }
  }
  return errors;
}

export function compareMaintainability(measured, baseline) {
  const errors = [];
  const exceptions = new Map();
  for (const entry of baseline) {
    const id = metricKey(entry.file, entry.function, entry.rule);
    if (exceptions.has(id)) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} duplicate exception`);
      continue;
    }
    exceptions.set(id, entry);
    if (!MAINTAINABILITY_RULES.has(entry.rule)) errors.push(`${entry.file} invalid rule=${entry.rule}`);
    if (typeof entry.reason !== 'string' || !entry.reason.trim() || entry.reason.startsWith('TODO:')) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} missing reason`);
    }
    if (typeof entry.owner !== 'string' || !entry.owner.trim() || entry.owner.startsWith('TODO:')) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} missing owner`);
    }
    if (!Number.isInteger(entry.allowed) || entry.allowed < 0) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} invalid allowed value`);
    }
  }

  const actual = new Map(measured);
  for (const [id, entry] of exceptions) {
    const value = actual.get(id);
    if (value === undefined) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} stale exception: no violation; remove it`);
    } else {
      if (value > entry.allowed) {
        errors.push(`${entry.file} :: ${entry.function} ${entry.rule} measured=${value} allowed=${entry.allowed}`);
      } else if (value < entry.allowed) {
        errors.push(`${entry.file} :: ${entry.function} ${entry.rule} stale measured=${value} allowed=${entry.allowed}; tighten`);
      }
      actual.delete(id);
    }
  }
  for (const [id, value] of actual) {
    const [file, fn, rule] = id.split('\0');
    errors.push(`${file} :: ${fn} ${rule} measured=${value} allowed=base threshold; fix or reviewed exception`);
  }
  return errors;
}

const FUNCTION_TYPES = new Set(['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression']);
const SCOPE_TYPES = new Set(['ClassDeclaration', 'ClassExpression', 'ObjectExpression']);

function children(node) {
  const result = [];
  for (const [key, value] of Object.entries(node ?? {})) {
    if (['parent', 'loc', 'range', 'tokens', 'comments'].includes(key)) continue;
    if (Array.isArray(value)) result.push(...value.filter(item => item && typeof item.type === 'string'));
    else if (value && typeof value.type === 'string') result.push(value);
  }
  return result;
}

function propertyName(node) {
  return node?.name ?? (typeof node?.value === 'string' ? node.value : undefined);
}

function qualified(parent, name) {
  return parent && name ? `${parent}.${name}` : name ?? parent ?? '';
}

function declarationName(node, parent) {
  if (node.id?.name) return node.id.name;
  if (parent?.type === 'VariableDeclarator') return propertyName(parent.id);
  if (parent?.type === 'AssignmentExpression') return propertyName(parent.left);
  if (['Property', 'MethodDefinition', 'PropertyDefinition'].includes(parent?.type)) {
    return propertyName(parent.key);
  }
  return undefined;
}

function scopeName(node, parent) {
  if (node.id?.name) return node.id.name;
  if (parent?.type === 'VariableDeclarator') return propertyName(parent.id);
  if (parent?.type === 'AssignmentExpression') return propertyName(parent.left);
  return undefined;
}

export function collectFunctionNodes(ast) {
  const functions = [];
  const duplicateIdentities = [];
  const seen = new Set();

  function visit(node, parentNode = null, scope = '', ordinals = new Map()) {
    if (!node || typeof node.type !== 'string') return;
    if (SCOPE_TYPES.has(node.type)) {
      const namedScope = scopeName(node, parentNode);
      const ordinal = (ordinals.get(scope) ?? 0) + 1;
      const anonymousScope = node.type === 'ObjectExpression'
        ? `<object#${ordinal}>`
        : `<class#${ordinal}>`;
      if (!namedScope) ordinals.set(scope, ordinal);
      const nestedScope = qualified(scope, namedScope ?? anonymousScope);
      const scopeOrdinals = new Map();
      for (const child of children(node)) visit(child, node, nestedScope, scopeOrdinals);
      return;
    }
    if (node.type === 'PropertyDefinition' && node.value) {
      const name = qualified(scope, `<field:${propertyName(node.key) ?? 'computed'}>`);
      functions.push({ node, name, start: node.loc.start, end: node.loc.end, implicit: true, displayName: 'class field initializer' });
      visit(node.value, node, name, new Map());
      return;
    }
    if (node.type === 'StaticBlock') {
      const ordinal = (ordinals.get(`${scope}:static`) ?? 0) + 1;
      ordinals.set(`${scope}:static`, ordinal);
      const name = qualified(scope, `<static#${ordinal}>`);
      functions.push({ node, name, start: node.loc.start, end: node.loc.end, implicit: true, displayName: 'class static block' });
      for (const child of children(node)) visit(child, node, name, new Map());
      return;
    }
    if (FUNCTION_TYPES.has(node.type)) {
      const named = declarationName(node, parentNode);
      const ordinal = (ordinals.get(scope) ?? 0) + 1;
      ordinals.set(scope, ordinal);
      const functionName = named ?? `<anonymous#${ordinal}>`;
      const baseIdentity = qualified(scope, functionName);
      let identity = baseIdentity;
      if (seen.has(identity)) {
        duplicateIdentities.push(baseIdentity);
        let suffix = 2;
        while (seen.has(`${baseIdentity}<duplicate#${suffix}>`)) suffix++;
        identity = `${baseIdentity}<duplicate#${suffix}>`;
      }
      seen.add(identity);
      functions.push({ node, name: identity, start: node.loc.start, end: node.loc.end });
      const nestedOrdinals = new Map();
      for (const parameter of node.params ?? []) visit(parameter, node, identity, nestedOrdinals);
      if (node.body) visit(node.body, node, identity, nestedOrdinals);
      return;
    }
    for (const child of children(node)) visit(child, node, scope, ordinals);
  }

  visit(ast);
  return { functions, duplicateIdentities };
}

function functionForReport(node, functions, reportLoc) {
  if (reportLoc?.start) {
    const implicit = functions.find(entry => entry.implicit && entry.start.line === reportLoc.start.line);
    if (implicit) return implicit;
  }
  const methodFunction = ['MethodDefinition', 'Property'].includes(node?.type) ? node.value : undefined;
  if (methodFunction) {
    const method = functions.find(entry => entry.node === methodFunction);
    if (method) return method;
  }
  let current = node;
  while (current) {
    const exact = functions.find(entry => entry.node === current);
    if (exact) return exact;
    if (current.type === 'PropertyDefinition' || current.type === 'StaticBlock') {
      const implicit = functions.find(entry => entry.node === current);
      if (implicit) return implicit;
    }
    if (current.type === 'ClassBody') return undefined;
    current = current.parent;
  }
  return undefined;
}

const RULE_OPTIONS = {
  complexity: 0,
  'max-depth': 0,
  'max-lines-per-function': { max: 0, skipBlankLines: true, skipComments: true, IIFEs: true },
};

function coreRuleListener(context, state, ruleName) {
  const rule = builtinRules.get(ruleName);
  const proxy = Object.create(context);
  Object.defineProperty(proxy, 'options', { value: [RULE_OPTIONS[ruleName]] });
  Object.defineProperty(proxy, 'report', {
    value: descriptor => collectRuleReport(context, state, ruleName, descriptor),
  });
  return rule.create(proxy);
}

function collectRuleReport(context, state, ruleName, descriptor) {
  const fn = functionForReport(descriptor.node, state.functions, descriptor.loc);
  const identity = fn?.name;
  const data = descriptor.data ?? {};
  const value = data.complexity ?? data.depth ?? data.lineCount;
  if (!identity || !Number.isInteger(value)) {
    context.report({ node: descriptor.node, message: `Unable to attribute ${ruleName} report to function` });
    return;
  }
  const key = metricKey(state.file, identity, ruleName);
  const measuredValue = Math.max(state.measured.get(key) ?? 0, value);
  state.measured.set(key, measuredValue);
  if (fn.displayName) {
    const displayedKey = metricKey(state.file, fn.displayName, ruleName);
    state.measured.set(displayedKey, Math.max(state.measured.get(displayedKey) ?? 0, measuredValue));
  }
}

function exactCoreListeners(context, state) {
  const listeners = Object.keys(RULE_OPTIONS)
    .map(rule => coreRuleListener(context, state, rule));
  const combined = {};
  for (const listener of listeners) {
    for (const [selector, handler] of Object.entries(listener)) {
      (combined[selector] ??= []).push(handler);
    }
  }
  return combined;
}

export const maintainabilityPlugin = {
  rules: {
    metrics: {
      meta: { type: 'problem', schema: [], messages: { violation: '{{payload}}' } },
      create(context) {
        const state = { file: '', functions: [], measured: new Map() };
        const merged = exactCoreListeners(context, state);
        const result = {};
        for (const [selector, handlers] of Object.entries(merged)) {
          result[selector] = (...args) => handlers.forEach(handler => handler(...args));
        }
        (merged.Program ??= []).push(node => {
          const normalizedFilename = context.filename.replaceAll('\\', '/');
          const sourceIndex = normalizedFilename.lastIndexOf('/src/');
          state.file = sourceIndex >= 0 ? normalizedFilename.slice(sourceIndex + 1) : normalizedFilename;
          const collected = collectFunctionNodes(node);
          state.functions = collected.functions;
          for (const duplicate of collected.duplicateIdentities) {
            context.report({
              node,
              messageId: 'violation',
              data: { payload: JSON.stringify({ file: state.file, duplicateIdentity: duplicate, violations: [] }) },
            });
          }
        });
        result.Program = (...args) => {
          for (const handler of merged.Program ?? []) handler(...args);
        };
        result['Program:exit'] = () => {
          for (const [id, value] of state.measured) {
            const [file, fn, rule] = id.split('\0');
            if (value <= MAINTAINABILITY_THRESHOLDS[rule]) continue;
            const functionEntry = state.functions.find(item => item.name === fn);
            context.report({
              node: functionEntry?.node ?? context.sourceCode.ast,
              message: JSON.stringify({ file, function: fn, displayFunction: functionEntry?.displayName ?? fn, violations: [rule], [rule]: value }),
            });
          }
        };
        return result;
      },
    },
  },
};
