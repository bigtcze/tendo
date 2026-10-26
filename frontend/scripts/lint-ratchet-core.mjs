export const MAINTAINABILITY_THRESHOLDS = Object.freeze({
  complexity: 15,
  'max-depth': 4,
  'max-lines-per-function': 80,
});

export const MAINTAINABILITY_RULES = new Set(Object.keys(MAINTAINABILITY_THRESHOLDS));

export function compareMaintainability(measured, baseline) {
  const errors = [];
  const entries = new Map();
  for (const entry of baseline) {
    const id = `${entry.file}\0${entry.rule}`;
    if (entries.has(id)) {
      errors.push(`${entry.file} ${entry.rule} duplicate exception`);
      continue;
    }
    entries.set(id, entry);
    if (!MAINTAINABILITY_RULES.has(entry.rule)) errors.push(`${entry.file} invalid rule=${entry.rule}`);
    if (typeof entry.reason !== 'string' || entry.reason.trim() === '' || entry.reason.startsWith('TODO:')) {
      errors.push(`${entry.file} ${entry.rule} missing reason`);
    }
    if (typeof entry.owner !== 'string' || entry.owner.trim() === '' || entry.owner.startsWith('TODO:')) {
      errors.push(`${entry.file} ${entry.rule} missing owner`);
    }
    if (!Number.isInteger(entry.allowed) || entry.allowed < 0) errors.push(`${entry.file} ${entry.rule} invalid allowed value`);
  }

  const observed = new Map(measured);
  for (const [id, entry] of entries) {
    const value = observed.get(id);
    if (value === undefined) {
      errors.push(`${entry.file} ${entry.rule} stale exception: no violation; remove it`);
      continue;
    }
    if (value > entry.allowed) errors.push(`${entry.file} ${entry.rule} measured=${value} allowed=${entry.allowed}`);
    else if (value < entry.allowed) errors.push(`${entry.file} ${entry.rule} stale measured=${value} allowed=${entry.allowed}; tighten`);
    observed.delete(id);
  }

  for (const [id, value] of observed) {
    const [file, rule] = id.split('\0');
    errors.push(`${file} ${rule} measured=${value} allowed=base threshold; add a reviewed exception or fix`);
  }
  return errors;
}
