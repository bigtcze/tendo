export const MAINTAINABILITY_THRESHOLDS = Object.freeze({
  complexity: 15,
  'max-depth': 4,
  'max-lines-per-function': 80,
});
export const MAINTAINABILITY_RULES = new Set(Object.keys(MAINTAINABILITY_THRESHOLDS));

export function metricKey(file, fn, rule) {
  return `${file}\0${fn}\0${rule}`;
}

export function compareAgainstBase(current, base) {
  const previous = new Map(
    base.map(entry => [metricKey(entry.file, entry.function, entry.rule), entry]),
  );
  const errors = [];
  for (const entry of current) {
    const key = metricKey(entry.file, entry.function, entry.rule);
    const old = previous.get(key);
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
    const key = metricKey(entry.file, entry.function, entry.rule);
    if (exceptions.has(key)) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} duplicate exception`);
      continue;
    }
    exceptions.set(key, entry);
    if (!MAINTAINABILITY_RULES.has(entry.rule)) {
      errors.push(`${entry.file} invalid rule=${entry.rule}`);
    }
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
  for (const [key, entry] of exceptions) {
    const value = actual.get(key);
    if (value === undefined) {
      errors.push(`${entry.file} :: ${entry.function} ${entry.rule} stale exception: no violation; remove it`);
    } else {
      if (value > entry.allowed) {
        errors.push(`${entry.file} :: ${entry.function} ${entry.rule} measured=${value} allowed=${entry.allowed}`);
      } else if (value < entry.allowed) {
        errors.push(`${entry.file} :: ${entry.function} ${entry.rule} stale measured=${value} allowed=${entry.allowed}; tighten`);
      }
      actual.delete(key);
    }
  }
  for (const [key, value] of actual) {
    const [file, fn, rule] = key.split('\0');
    errors.push(`${file} :: ${fn} ${rule} measured=${value} allowed=base threshold; fix or reviewed exception`);
  }
  return errors;
}

function fnType(node) {
  return ['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression'].includes(node?.type);
}
function children(node) {
  const found = [];
  for (const [key, value] of Object.entries(node ?? {})) {
    if (['parent', 'loc', 'range', 'tokens', 'comments'].includes(key)) continue;
    if (Array.isArray(value)) {
      found.push(...value.filter(item => item && typeof item.type === 'string'));
    }
    else if (value && typeof value.type === 'string') found.push(value);
  }
  return found;
}
function countsComplexity(node) {
  const branch = ['IfStatement', 'ForStatement', 'ForOfStatement', 'WhileStatement', 'DoWhileStatement'].includes(node.type);
  const caseBranch = node.type === 'SwitchCase' && Boolean(node.test);
  return branch || caseBranch || node.type === 'LogicalExpression';
}

function functionName(node) {
  const parent = node.parent;
  const declarationName = node.id?.name;
  const variableName = parent?.type === 'VariableDeclarator' ? parent.id.name : undefined;
  const propertyName = ['Property', 'MethodDefinition', 'PropertyDefinition'].includes(parent?.type)
    ? parent.key.name ?? parent.key.value
    : undefined;
  return declarationName ?? variableName ?? propertyName ?? null;
}

export function collectFunctionMetrics(ast) {
  const output = [];
  function collect(node, parentName = '', siblingOrdinals = new Map()) {
    if (!node || typeof node.type !== 'string') return;
    if (fnType(node)) {
      const named = functionName(node);
      const ordinal = (siblingOrdinals.get(parentName) ?? 0) + 1;
      siblingOrdinals.set(parentName, ordinal);
      const name = named ?? `<anonymous#${ordinal}>`;
      const qualified = parentName ? `${parentName}.${name}` : name;
      let complexity = 1;
      let nesting = 0;
      const controlled = new Set([
        'IfStatement', 'ForStatement', 'ForOfStatement',
        'WhileStatement', 'DoWhileStatement', 'SwitchStatement',
        'SwitchCase', 'CatchClause',
      ]);
      function walk(current, depth = 0) {
        if (current !== node && fnType(current)) return;
        if (countsComplexity(current)) complexity++;
        const next = depth + (controlled.has(current.type) ? 1 : 0);
        nesting = Math.max(nesting, next);
        for (const child of children(current)) walk(child, next);
      }
      walk(node.body);
      output.push({
        file: '',
        function: qualified,
        complexity,
        'max-depth': nesting,
        'max-lines-per-function': node.body.loc.end.line - node.body.loc.start.line + 1,
      });
      for (const child of children(node.body)) collect(child, qualified, new Map());
      return;
    }
    for (const child of children(node)) collect(child, parentName, siblingOrdinals);
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
            const violations = Object.entries(MAINTAINABILITY_THRESHOLDS)
              .filter(([rule, limit]) => result[rule] > limit)
              .map(([rule]) => rule);
            if (violations.length) context.report({ node, messageId: 'violation', data: { payload: JSON.stringify({ ...result, violations }) } });
          }
        } };
      },
    },
  },
};
