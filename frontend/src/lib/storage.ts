// Per-machine conveniences only (last DSN, query, history); the app works without them.
export function load<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem("dbdrishti:" + key);
    return raw == null ? fallback : (JSON.parse(raw) as T);
  } catch {
    return fallback;
  }
}

export function save(key: string, value: unknown) {
  try {
    localStorage.setItem("dbdrishti:" + key, JSON.stringify(value));
  } catch {
    // storage unavailable: nothing to remember
  }
}
