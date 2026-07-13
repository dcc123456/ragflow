import dayjs from 'dayjs';
import storage from './authorization-util';

// --- Timezone version tracking for reactive updates ---
// When the timezone changes, the version increments and all subscribers
// are notified. Components using useTimezoneVersion() will re-render.
let timezoneVersion = 0;
const timezoneSubscribers = new Set<() => void>();

export function subscribeTimezone(callback: () => void): () => void {
  timezoneSubscribers.add(callback);
  return () => {
    timezoneSubscribers.delete(callback);
  };
}

export function getTimezoneVersion(): number {
  return timezoneVersion;
}
// --- End timezone version tracking ---

/**
 * Parse the backend timezone string into an IANA timezone name.
 * Backend format: "UTC+8\tAsia/Shanghai" (tab-separated)
 * Frontend format: "GMT+08:00 Asia/Shanghai" (space-separated)
 * Pure IANA format: "Asia/Shanghai"
 *
 * @param tz - Raw timezone string from backend or null/undefined
 * @returns IANA timezone name (e.g. "Asia/Shanghai"); falls back to browser timezone on failure
 */
export function parseTimezone(tz: string | undefined | null): string {
  if (!tz) return getBrowserTimezone();
  const parts = tz.split(/\t|\s+/);
  const ianaName = parts.length > 1 ? parts[parts.length - 1] : tz;
  try {
    Intl.DateTimeFormat('en-US', { timeZone: ianaName });
    return ianaName;
  } catch {
    return getBrowserTimezone();
  }
}

/**
 * Get the browser's default IANA timezone.
 * @returns IANA timezone name, or 'UTC' if unavailable
 */
export function getBrowserTimezone(): string {
  return new Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
}

/**
 * Apply timezone globally: persist to localStorage and set dayjs default.
 * Called at app startup and after fetching user info.
 * @param timezone - Raw timezone string (may be in backend or IANA format)
 */
export function applyTimezone(timezone: string): void {
  const ianaName = parseTimezone(timezone);
  const previousTz = storage.getTimezone();
  storage.setTimezone(ianaName);
  dayjs.tz.setDefault(ianaName);
  if (ianaName !== previousTz) {
    timezoneVersion++;
    timezoneSubscribers.forEach((cb) => cb());
  }
}

/**
 * Get the currently effective timezone from storage, with browser fallback.
 * Used by formatDate when no explicit timezone is passed.
 * @returns IANA timezone name
 */
export function getCurrentTimezone(): string {
  const stored = storage.getTimezone();
  if (stored) {
    try {
      Intl.DateTimeFormat('en-US', { timeZone: stored });
      return stored;
    } catch {
      // stored value invalid, fall through to browser default
    }
  }
  return getBrowserTimezone();
}
