/** Counts every `Intl.DateTimeFormat` construction (with or without `new`) until restored. */
export function countDateTimeFormats() {
  const original = Intl.DateTimeFormat;
  const counter = { count: 0, restore: () => { Intl.DateTimeFormat = original; } };
  Intl.DateTimeFormat = new Proxy(original, {
    construct(target, args) { counter.count += 1; return Reflect.construct(target, args); },
    apply(target, self, args) { counter.count += 1; return Reflect.apply(target, self, args); },
  });
  return counter;
}
