import { useCallback, useMemo, useState } from 'react';

import { endOfDay, startOfDay, subDays } from 'date-fns';

import Spotlight from '@/components/spotlight';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { ScrollArea } from '@/components/ui/scroll-area';
import type { DateRange } from '@/components/originui/calendar';

import { toTokenStatsWindow, useTokenStatsUsers } from './hooks';
import { OverView } from './overview';
import { RankingTable } from './ranking-table';
import { ModelUsageToolbar } from './table-toolbar';
import type { TokenStatsGranularity } from './types';
import { UsageDetail } from './usage-detail';

const DefaultTopN = 10;
const DefaultGranularity: TokenStatsGranularity = 'day';

function getDefaultRange(): DateRange {
  const today = new Date();
  return {
    from: startOfDay(subDays(today, 6)),
    to: endOfDay(today),
  };
}

export default function ModelUsageStatistics() {
  const [range, setRange] = useState<DateRange>(getDefaultRange);
  const [top, setTop] = useState(DefaultTopN);
  const [granularity, setGranularity] =
    useState<TokenStatsGranularity>(DefaultGranularity);
  const [selectedEmail, setSelectedEmail] = useState('');

  const window = useMemo(() => toTokenStatsWindow(range), [range]);

  const { data: users, isLoading } = useTokenStatsUsers(window, top);
  const activeEmail = selectedEmail || users?.[0]?.email || '';

  const handleRangeChange = useCallback((nextRange: DateRange) => {
    setRange({
      from: startOfDay(nextRange.from),
      to: endOfDay(nextRange.to ?? nextRange.from),
    });
  }, []);

  return (
    <Card className="!shadow-none relative h-full bg-transparent rounded-xl overflow-hidden">
      <Spotlight />

      <ScrollArea className="size-full">
        <CardHeader>
          <ModelUsageToolbar range={range} onRangeChange={handleRangeChange} />
        </CardHeader>

        <CardContent>
          <div className="grid grid-cols-1 gap-8 xl:grid-cols-[minmax(0,1fr)_400px]">
            <div className="min-w-0 space-y-8">
              <OverView window={window} />

              <UsageDetail
                window={window}
                users={users ?? []}
                email={activeEmail}
                onEmailChange={setSelectedEmail}
                granularity={granularity}
                onGranularityChange={setGranularity}
              />
            </div>
            <div className="min-w-0 border rounded-sm p-2">
              <RankingTable
                items={users ?? []}
                isLoading={isLoading}
                top={top}
                onTopChange={setTop}
                selectedEmail={activeEmail}
                onSelect={setSelectedEmail}
              />
            </div>
          </div>
        </CardContent>
      </ScrollArea>
    </Card>
  );
}
