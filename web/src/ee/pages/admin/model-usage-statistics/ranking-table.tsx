import { memo } from 'react';
import { useTranslation } from 'react-i18next';

import { SelectWithSearch } from '@/components/originui/select-with-search';
import { TableEmpty } from '@/components/table-skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { cn } from '@/lib/utils';

import type { TokenStatsUserItem } from './types';
import { formatCount } from './utils';

const TopNOptions = [10, 20, 50];

interface RankingRowProps {
  rank: number;
  item: TokenStatsUserItem;
  selected: boolean;
  onSelect: (email: string) => void;
}

const RankingRow = memo(
  ({ rank, item, selected, onSelect }: RankingRowProps) => {
    const handleClick = () => {
      onSelect(item.email);
    };

    return (
      <TableRow
        className={cn(
          'group cursor-pointer hover:bg-bg-card',
          selected && 'bg-bg-card',
        )}
        onClick={handleClick}
        aria-selected={selected}
      >
        <TableCell className="w-16 text-text-secondary">{rank}</TableCell>
        <TableCell className="text-text-secondary group-hover:text-text-primary">
          <div className="truncate" title={item.email}>
            {item.email}
          </div>
        </TableCell>
        <TableCell className="text-text-secondary group-hover:text-text-primary">
          {formatCount(item.total_tokens)}
        </TableCell>
      </TableRow>
    );
  },
);

RankingRow.displayName = 'RankingRow';

interface RankingTableProps {
  items: TokenStatsUserItem[];
  isLoading: boolean;
  top: number;
  onTopChange: (top: number) => void;
  selectedEmail: string;
  onSelect: (email: string) => void;
}

export function RankingTable({
  items,
  isLoading,
  top,
  onTopChange,
  selectedEmail,
  onSelect,
}: RankingTableProps) {
  const { t } = useTranslation();

  const handleTopChange = (value: string) => {
    onTopChange(Number(value));
  };

  return (
    <section>
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-base font-medium text-text-primary">
          {t('admin.modelUsage.rankList')}
        </h2>

        <SelectWithSearch
          value={top.toString()}
          options={TopNOptions.map((option) => ({
            value: option.toString(),
            label: t('admin.modelUsage.topN', { count: option }),
          }))}
          onChange={handleTopChange}
          triggerClassName="w-[130px]"
        />
      </div>

      <Table>
        <colgroup>
          <col className="w-14" />
          <col />
          <col className="w-24" />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHead>{t('admin.modelUsage.rank')}</TableHead>
            <TableHead>{t('admin.modelUsage.email')}</TableHead>
            <TableHead>{t('admin.modelUsage.tokens')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading ? (
            <TableRow>
              <TableCell
                colSpan={3}
                className="h-24 text-center text-text-secondary"
              >
                {t('common.loading')}
              </TableCell>
            </TableRow>
          ) : items.length ? (
            items.map((item, index) => (
              <RankingRow
                key={item.user_id || item.email}
                rank={index + 1}
                item={item}
                selected={item.email === selectedEmail}
                onSelect={onSelect}
              />
            ))
          ) : (
            <TableEmpty columnsLength={3} />
          )}
        </TableBody>
      </Table>
    </section>
  );
}
