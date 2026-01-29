import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { SearchInput } from '@/components/ui/input';
import { useSetModalState } from '@/hooks/common-hooks';
import { IEvaluationRun } from '@/interfaces/database/evaluation';
import { cn } from '@/lib/utils';
import { useDebounce } from 'ahooks';
import { PanelLeftClose, PanelRightClose, Plus } from 'lucide-react';
import React, { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { EvaluationType } from './constants';

type EvaluationListProps = {
  runs: IEvaluationRun[];
  selectedRunId: string;
  onSelect: (runId: string) => void;
  type: EvaluationType;
};

export function EvaluationList({
  runs,
  selectedRunId,
  onSelect,
  type,
}: EvaluationListProps) {
  const { t } = useTranslation();
  const { visible, switchVisible } = useSetModalState(true);
  const [searchString, setSearchString] = React.useState('');

  const debouncedSearchString = useDebounce(searchString, { wait: 500 });

  const filteredRuns = useMemo(() => {
    return runs.filter((run) => {
      const matchesSearch = run.name
        .toLowerCase()
        .includes(debouncedSearchString.toLowerCase());
      return matchesSearch;
    });
  }, [runs, debouncedSearchString]);

  const getStatusBadgeVariant = (status: string) => {
    switch (status) {
      case 'COMPLETED':
        return 'default';
      case 'RUNNING':
        return 'secondary';
      case 'FAILED':
        return 'destructive';
      default:
        return 'outline';
    }
  };

  const handleCardClick = useCallback(
    (runId: string) => () => {
      onSelect(runId);
    },
    [onSelect],
  );

  if (!visible) {
    return (
      <PanelRightClose
        className="cursor-pointer size-4 mt-8"
        onClick={switchVisible}
      />
    );
  }

  return (
    <section className="p-6 w-[296px] flex flex-col">
      <section className="flex items-center text-base justify-between gap-2">
        <div className="flex gap-3 items-center min-w-0">
          <span className="flex-1 truncate">
            {type === EvaluationType.Agent
              ? t('evaluation.agentEvaluation')
              : t('evaluation.chatEvaluation')}
          </span>
        </div>
        <PanelLeftClose
          className="cursor-pointer size-4"
          onClick={switchVisible}
        />
      </section>
      <div className="flex justify-between items-center mb-4 pt-10">
        <span className="text-base font-bold">{t('evaluation.title')}</span>
        <Button variant={'ghost'}>
          <Plus></Plus>
        </Button>
      </div>
      <div className="pb-4">
        <SearchInput
          onChange={(e) => setSearchString(e.target.value)}
          value={searchString}
        ></SearchInput>
      </div>
      <div className="space-y-4 flex-1 overflow-auto">
        {filteredRuns.map((x) => (
          <Card
            key={x.id}
            onClick={handleCardClick(x.id!)}
            className={cn('cursor-pointer bg-transparent', {
              'bg-bg-card': selectedRunId === x.id,
            })}
          >
            <CardContent className="px-3 py-2 flex justify-between items-center group gap-1">
              <div className="truncate flex-1">{x.name}</div>
              <Badge variant={getStatusBadgeVariant(x.status)}>
                {x.status}
              </Badge>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
}
