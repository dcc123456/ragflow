import { useSyncExternalStore } from 'react';
import { getTimezoneVersion, subscribeTimezone } from '@/utils/timezone';

export function useTimezoneVersion(): number {
  return useSyncExternalStore(
    subscribeTimezone,
    getTimezoneVersion,
    getTimezoneVersion,
  );
}
