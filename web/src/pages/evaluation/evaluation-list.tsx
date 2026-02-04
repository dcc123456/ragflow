import { MoreButton } from '@/components/more-button';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { SearchInput } from '@/components/ui/input';
import { useSetModalState } from '@/hooks/common-hooks';
import { useFetchAgent } from '@/hooks/use-agent-request';
import { useFetchDialog } from '@/hooks/use-chat-request';
import { useFetchEvaluationRunList } from '@/hooks/use-evaluation-request';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { cn } from '@/lib/utils';
import { useDebounce } from 'ahooks';
import { get } from 'lodash';
import { PanelRightClose, Plus } from 'lucide-react';
import React, { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { EvaluationType } from './constants';
import { EvaluationRunDropdown } from './evaluation-run-dropdown';

type EvaluationListProps = {
  selectedRunId: string;
  type: EvaluationType;
};

export function EvaluationList({ selectedRunId, type }: EvaluationListProps) {
  const { t } = useTranslation();
  const { visible, switchVisible } = useSetModalState(true);
  const [searchString, setSearchString] = React.useState('');
  const { setRunId, setPage } = useEvaluationUrl();

  const { data: runList } = useFetchEvaluationRunList();
  const useFetchData =
    type === EvaluationType.Chat ? useFetchDialog : useFetchAgent;

  const { data } = useFetchData();

  const name = get(data, 'name');
  const icon = get(data, 'icon');

  const debouncedSearchString = useDebounce(searchString, { wait: 500 });

  const filteredRuns = useMemo(() => {
    return runList?.runs.filter((run) => {
      const matchesSearch = run.name
        .toLowerCase()
        .includes(debouncedSearchString.toLowerCase());
      return matchesSearch;
    });
  }, [runList?.runs, debouncedSearchString]);

  const handleCardClick = useCallback(
    (runId: string) => () => {
      setRunId(runId);
      setPage('1');
    },
    [setPage, setRunId],
  );

  const handleAddClick = useCallback(() => {
    setRunId('');
  }, [setRunId]);

  if (!visible) {
    return (
      <PanelRightClose
        className="cursor-pointer size-4 mt-8 ml-6"
        onClick={switchVisible}
      />
    );
  }

  return (
    <section className="p-6 w-[296px] flex flex-col">
      <section className="flex items-center text-base justify-between gap-2">
        <div className="flex gap-3 items-center min-w-0">
          <RAGFlowAvatar
            avatar={icon}
            name={name}
            className="size-8 cursor-pointer"
            onClick={switchVisible}
          ></RAGFlowAvatar>
          <span className="flex-1 truncate">{name}</span>
        </div>
      </section>
      <div className="flex justify-between items-center mb-4 pt-10">
        <div className="space-x-3">
          <span className="text-base">{t('evaluation.title')}</span>
          <span className="text-text-secondary text-xs">{runList.total}</span>
        </div>
        <Button variant={'ghost'} onClick={handleAddClick}>
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
              <EvaluationRunDropdown runId={x.id!}>
                <MoreButton />
              </EvaluationRunDropdown>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
}
