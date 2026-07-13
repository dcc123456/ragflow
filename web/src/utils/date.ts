import dayjs from 'dayjs';
import { getCurrentTimezone } from './timezone';

/**
 * Convert various date input formats to a zoned dayjs object.
 *
 * Handles:
 * - Unix timestamp in seconds (10-digit): dayjs.unix(n)
 * - Unix timestamp in milliseconds (13-digit): dayjs(n)
 * - ISO string (with or without Z): treated as UTC then converted
 * - Date object / dayjs object: directly .tz()
 *
 * @param date - Date input of unknown type
 * @param tz - Optional IANA timezone name; defaults to getCurrentTimezone()
 * @returns dayjs.Dayjs in the target timezone, or null if input is empty/invalid
 */
function toZonedDayjs(date: unknown, tz?: string): dayjs.Dayjs | null {
  if (!date) return null;

  const targetTz = tz || getCurrentTimezone();

  if (typeof date === 'number') {
    // Detect seconds-level timestamp (range ~1e8 to ~1e11)
    if (date > 1e8 && date < 1e11) {
      return dayjs.unix(date).tz(targetTz);
    }
    return dayjs(date).tz(targetTz);
  }

  if (typeof date === 'string') {
    const parsed = dayjs.utc(date);
    if (!parsed.isValid()) return null;
    return parsed.tz(targetTz);
  }

  if (date instanceof Date) {
    return dayjs(date).tz(targetTz);
  }

  if (dayjs.isDayjs(date)) {
    return date.tz(targetTz);
  }

  return null;
}

export function formatDate(date: any, format?: string) {
  const thisFormat = format || 'DD/MM/YYYY HH:mm:ss';
  const d = toZonedDayjs(date);
  if (!d) return '';
  return d.format(thisFormat);
}

export function formatTime(date: any) {
  const d = toZonedDayjs(date);
  if (!d) return '';
  return d.format('HH:mm:ss');
}

export function today() {
  return formatDate(dayjs());
}

export function lastDay() {
  return formatDate(dayjs().subtract(1, 'days'));
}

export function lastWeek() {
  return formatDate(dayjs().subtract(1, 'weeks'));
}

export function formatPureDate(date: any) {
  const d = toZonedDayjs(date);
  if (!d) return '';
  return d.format('DD/MM/YYYY');
}

export function formatStandardDate(date: any) {
  const d = toZonedDayjs(date);
  if (!d || !d.isValid()) return '';
  return d.format('YYYY-MM-DD');
}

export function formatSecondsToHumanReadable(seconds: number): string {
  if (isNaN(seconds) || seconds < 0) {
    return '0s';
  }

  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  // const s = toFixed(seconds % 60, 3);
  const s = seconds % 60;
  const formattedSeconds = s === 0 ? '0' : s.toFixed(3).replace(/\.?0+$/, '');
  const parts = [];
  if (h > 0) parts.push(`${h}h `);
  if (m > 0) parts.push(`${m}m `);
  if (s || parts.length === 0) parts.push(`${formattedSeconds}s`);

  return parts.join('');
}
