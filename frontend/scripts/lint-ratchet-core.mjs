export const MAINTAINABILITY_THRESHOLDS = Object.freeze({ complexity: 15, 'max-depth': 4, 'max-lines-per-function': 80 });
export const MAINTAINABILITY_RULES = new Set(Object.keys(MAINTAINABILITY_THRESHOLDS));

export function metricKey(file, fn, rule) { return `${file}\0${fn}\0${rule}`; }

export function compareMaintainability(measured, baseline) {
  const errors = [];
  const exceptions = new Map();
  for (const entry of baseline) {
    const key = metricKey(entry.file, entry.function, entry.rule);
    if (exceptions.has(key)) { errors.push(`${entry.file} :: ${entry.function} ${entry.rule} duplicate exception`); continue; }
    exceptions.set(key, entry);
    if (!MAINTAINABILITY_RULES.has(entry.rule)) errors.push(`${entry.file} invalid rule=${entry.rule}`);
    if (typeof entry.reason !== 'string' || !entry.reason.trim() || entry.reason.startsWith('TODO:')) errors.push(`${entry.file} :: ${entry.function} ${entry.rule} missing reason`);
    if (typeof entry.owner !== 'string' || !entry.owner.trim() || entry.owner.startsWith('TODO:')) errors.push(`${entry.file} :: ${entry.function} ${entry.rule} missing owner`);
    if (!Number.isInteger(entry.allowed) || entry.allowed < 0) errors.push(`${entry.file} :: ${entry.function} ${entry.rule} invalid allowed value`);
  }
  const actual = new Map(measured);
  for (const [key, entry] of exceptions) {
    const value = actual.get(key);
    if (value === undefined) errors.push(`${entry.file} :: ${entry.function} ${entry.rule} stale exception: no violation; remove it`);
    else {
      if (value > entry.allowed) errors.push(`${entry.file} :: ${entry.function} ${entry.rule} measured=${value} allowed=${entry.allowed}`);
      else if (value < entry.allowed) errors.push(`${entry.file} :: ${entry.function} ${entry.rule} stale measured=${value} allowed=${entry.allowed}; tighten`);
      actual.delete(key);
    }
  }
  for (const [key, value] of actual) {
    const [file, fn, rule] = key.split('\0');
    errors.push(`${file} :: ${fn} ${rule} measured=${value} allowed=base threshold; fix or reviewed exception`);
  }
  return errors;
}

function fnType(node) { return ['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression'].includes(node?.type); }
function children(node) {
  const found = [];
  for (const [key, value] of Object.entries(node ?? {})) {
    if (['parent', 'loc', 'range', 'tokens', 'comments'].includes(key)) continue;
    if (Array.isArray(value)) found.push(...value.filter(item => item && typeof item.type === 'string'));
    else if (value && typeof value.type === 'string') found.push(value);
  }
  return found;
}
function functionName(node, ordinal) {
  const parent = node.parent;
  return node.id?.name
    ?? (parent?.type === 'VariableDeclarator' ? parent.id.name : undefined)
    ?? (['Property', 'MethodDefinition', 'PropertyDefinition'].includes(parent?.type) ? parent.key.name ?? parent.key.value : undefined)
    ?? `<anonymous#${ordinal}>`;
}

export function collectFunctionMetrics(ast) {
  const output = [];
  function collect(node, parentName = '', anonymous = { value: 0 }) {
    if (!node || typeof node.type !== 'string') return;
    if (fnType(node)) {
      const name = functionName(node, ++anonymous.value);
      const qualified = parentName ? `${parentName}.${name}` : name;
      let complexity = 1;
      let nesting = 0;
      const controlled = new Set(['IfStatement', 'ForStatement', 'ForOfStatement', 'WhileStatement', 'DoWhileStatement', 'SwitchStatement', 'SwitchCase', 'CatchClause']);
      function walk(current, depth = 0) {
        if (current !== node && fnType(current)) return;
        if (['IfStatement', 'ForStatement', 'ForOfStatement', 'WhileStatement', 'DoWhileStatement'].includes(current.type) || current.type === 'SwitchCase' && current.test || current.type === 'LogicalExpression') complexity++;
        const next = depth + (controlled.has(current.type) ? 1 : 0);
        nesting = Math.max(nesting, next);
        for (const child of children(current)) walk(child, next);
      }
      walk(node.body);
      output.push({ file: '', function: qualified, complexity, 'max-depth': nesting, 'max-lines-per-function': node.body.loc.end.line - node.body.loc.start.line + 1 });
      for (const child of children(node.body)) collect(child, qualified, { value: 0 });
      return;
    }
    for (const child of children(node)) collect(child, parentName, anonymous);
  }
  collect(ast);
  return output;
}

export const maintainabilityPlugin = {
  rules: {
    metrics: {
      meta: { type: 'problem', schema: [], messages: { violation: '{{payload}}' } },
      create(context) {
        return { Program(node) {
          const filename = context.filename.replaceAll('\\', '/').split('/frontend/').at(-1);
          for (const result of collectFunctionMetrics(node)) {
            result.file = filename;
            const violations = Object.entries(MAINTAINABILITY_THRESHOLDS).filter(([rule, limit]) => result[rule] > limit).map(([rule]) => rule);
            if (violations.length) context.report({ node, messageId: 'violation', data: { payload: JSON.stringify({ ...result, violations }) } });
          }
        } };
      },
    },
  },
};
