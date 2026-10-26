import { execFileSync } from 'node:child_process';
import { relative, sep } from 'node:path';
import { builtinRules } from 'eslint/use-at-your-own-risk';
import { createIdentityResolver } from './lint-ratchet-identity.mjs';

export const MAINTAINABILITY_THRESHOLDS = Object.freeze({
  complexity: 15,
  'max-depth': 4,
  'max-lines-per-function': 80,
});
export const MAINTAINABILITY_RULES = new Set(Object.keys(MAINTAINABILITY_THRESHOLDS));

const OPTIONS = {
  complexity: 0,
  'max-depth': 0,
  'max-lines-per-function': { max: 0, skipBlankLines: true, skipComments: true, IIFEs: true },
};

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
