import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { PropsRangeRequired } from 'react-day-picker';

import {
  endOfDay,
  endOfMonth,
  format,
  isSameDay,
  startOfDay,
  startOfMonth,
  subDays,
  subMonths,
} from 'date-fns';
import { CalendarIcon } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Calendar } from '@/components/ui/calendar';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import type { DateRange } from '@/components/originui/calendar';
import { cn } from '@/lib/utils';

const PresetDefinitions = [
  {
    key: 'today',
    build: (now: Date) => ({ from: startOfDay(now), to: endOfDay(now) }),
  },
  {
    key: 'yesterday',
    build: (now: Date) => ({
      from: startOfDay(subDays(now, 1)),
      to: endOfDay(subDays(now, 1)),
    }),
  },
  {
    key: 'last7Days',
    build: (now: Date) => ({
      from: startOfDay(subDays(now, 6)),
      to: endOfDay(now),
    }),
  },
  {
    key: 'last30Days',
    build: (now: Date) => ({
      from: startOfDay(subDays(now, 29)),
      to: endOfDay(now),
    }),
  },
  {
    key: 'last90Days',
    build: (now: Date) => ({
      from: startOfDay(subDays(now, 89)),
      to: endOfDay(now),
    }),
  },
  {
    key: 'thisMonth',
    build: (now: Date) => ({
      from: startOfMonth(now),
      to: endOfDay(now),
    }),
  },
  {
    key: 'lastMonth',
    build: (now: Date) => ({
      from: startOfMonth(subMonths(now, 1)),
      to: endOfMonth(subMonths(now, 1)),
    }),
  },
] as const;

type PresetKey = (typeof PresetDefinitions)[number]['key'];

interface UsageDateRangePickerProps {
  range: DateRange;
  onRangeChange: (range: DateRange) => void;
}

export function UsageDateRangePicker({
  range,
  onRangeChange,
}: UsageDateRangePickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);

  const presets = useMemo(() => {
    const now = new Date();
    return PresetDefinitions.map((preset) => ({
      key: preset.key,
      range: preset.build(now),
    }));
  }, []);

  const activePreset = presets.find(
    (preset) =>
      isSameDay(preset.range.from, range.from) &&
      range.to !== undefined &&
      isSameDay(preset.range.to, range.to),
  );

  const handlePresetClick = (key: PresetKey) => {
    const preset = presets.find((item) => item.key === key);
    if (!preset) {
      return;
    }
    onRangeChange(preset.range);
    setOpen(false);
  };

  const handleSelect: NonNullable<PropsRangeRequired['onSelect']> = (
    nextRange,
  ) => {
    if (!nextRange?.from) {
      return;
    }
    onRangeChange({ from: nextRange.from, to: nextRange.to });
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" className="justify-start px-2.5 font-normal">
          <CalendarIcon />
          {format(range.from, 'yyyy-MM-dd')} -{' '}
          {format(range.to ?? range.from, 'yyyy-MM-dd')}
        </Button>
      </PopoverTrigger>

      <PopoverContent className="w-auto p-0" align="end">
        <div className="flex max-sm:flex-col">
          <div className="flex flex-col gap-1 border-r border-border-button p-2 max-sm:border-r-0 max-sm:border-b">
            {presets.map((preset) => (
              <Button
                key={preset.key}
                variant="ghost"
                size="sm"
                className={cn(
                  'justify-start',
                  activePreset?.key === preset.key &&
                    'bg-bg-card text-text-primary',
                )}
                onClick={() => handlePresetClick(preset.key)}
              >
                {t(`admin.modelUsage.${preset.key}`)}
              </Button>
            ))}
          </div>

          <Calendar
            mode="range"
            required
            numberOfMonths={2}
            selected={range}
            onSelect={handleSelect}
          />
        </div>
      </PopoverContent>
    </Popover>
  );
}
