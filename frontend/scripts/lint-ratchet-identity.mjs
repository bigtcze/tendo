// Structural code-path ownership for the frontend maintainability ratchet.

const FUNCTIONS = new Set(['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression']);
const SCOPES = new Set(['ClassDeclaration', 'ClassExpression', 'ObjectExpression']);
const SKIP = new Set(['parent', 'loc', 'range', 'tokens', 'comments']);

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
    if (!node.computed) return propertyName(node.key) ?? `computed#${next(scope, 'computed')}`;
    // A computed key's runtime name is unknown; never reuse the expression text as a name.
    visit(node.key, qualified(scope, `key#${next(scope, 'key')}`));
    return `computed#${next(scope, 'computed')}`;
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
    if (!name && parent?.type === 'Property' && parent.kind === 'init' && !parent.computed) {
      name = propertyName(parent.key);
    }
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
