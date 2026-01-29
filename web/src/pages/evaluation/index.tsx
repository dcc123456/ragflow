import { PageHeader } from '@/components/page-header';
import { Button } from '@/components/ui/button';
import { useSetModalState } from '@/hooks/common-hooks';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import {
  useFetchEvaluationRunList,
  useFetchEvaluationRunResults,
} from '@/hooks/use-evaluation-request';
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { ArrowBigLeft, PanelRightClose, Upload } from 'lucide-react';
import { useParams } from 'react-router';
import { EvaluationType } from './constants';
import { EvaluationConfigPanel } from './evaluation-config-panel';
import { EvaluationList } from './evaluation-list';
import { EvaluationTable } from './evaluation-table';
import { RunDropdownButton } from './run-dropdown-button';

export default function Evaluation() {
  const { t } = useTranslation();
  const { id } = useParams();
  const { navigateToAgent, navigateToChat } = useNavigatePage();
  const { type, runId, setRunId } = useEvaluationUrl();
  const { visible: configVisible, switchVisible: switchConfigVisible } =
    useSetModalState(true);

  const { data: runList } = useFetchEvaluationRunList();

  const filteredRuns = useMemo(() => {
    if (!runList?.runs) return [];
    return runList.runs.filter((run) => run.target_type === type);
  }, [runList, type]);

  const { data: runResults } = useFetchEvaluationRunResults(runId);

  const currentRun = useMemo(() => {
    return filteredRuns.find((r) => r.id === runId);
  }, [filteredRuns, runId]);

  const handleBack = useCallback(() => {
    if (type === EvaluationType.Agent) {
      navigateToAgent(id!)();
    } else {
      navigateToChat(id!)();
    }
  }, [type, navigateToAgent, id, navigateToChat]);

  return (
    <section className="h-full flex flex-col">
      <PageHeader>
        <Button onClick={handleBack} variant={'ghost'}>
          <ArrowBigLeft /> Back
        </Button>
      </PageHeader>
      <div className="flex flex-1 min-h-0 pb-9 gap-4">
        <EvaluationList
          runs={filteredRuns}
          selectedRunId={runId}
          onSelect={setRunId}
          type={type}
        />

        <section className="flex-1 min-w-0">
          <section className="flex justify-between pb-4">
            <div>
              {currentRun?.name || t('evaluation.selectRun')}
              {currentRun && (
                <span className="ml-2 text-sm font-normal text-text-secondary">
                  {t('evaluation.totalCases', {
                    count: runResults?.cases?.length || 0,
                  })}
                </span>
              )}
            </div>
            <div className="space-x-4">
              <Button variant={'ghost'}>
                <Upload /> Export
              </Button>
              <RunDropdownButton></RunDropdownButton>
            </div>
          </section>
          <EvaluationTable runId={runId} type={type} results={runResults} />
        </section>
        {configVisible ? (
          <EvaluationConfigPanel
            type={type}
            visible={configVisible}
            onClose={switchConfigVisible}
          />
        ) : (
          <PanelRightClose
            className="size-4 cursor-pointer mr-5"
            onClick={switchConfigVisible}
          />
        )}
      </div>
    </section>
  );
}
