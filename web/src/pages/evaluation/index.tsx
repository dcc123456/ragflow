import { PageHeader } from '@/components/page-header';
import { Button } from '@/components/ui/button';
import { useSetModalState } from '@/hooks/common-hooks';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import {
  useExportEvaluationRun,
  useFetchEvaluationRun,
} from '@/hooks/use-evaluation-request';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { zodResolver } from '@hookform/resolvers/zod';
import { ArrowBigLeft, ListCheck, PanelRightClose, Upload } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useParams } from 'react-router';
import { EvaluationType } from './constants';
import { EvaluationConfigPanel } from './evaluation-config-panel';
import { EvaluationList } from './evaluation-list';
import {
  EvaluationSettingsFormType,
  useEvaluationSchema,
} from './evaluation-schemas';
import { EvaluationTable } from './evaluation-table';
import { RunDropdownButton } from './run-dropdown-button';
import { useApplyConfig } from './use-apply-config';

export default function Evaluation() {
  const { t } = useTranslation();
  const { id } = useParams();
  const { navigateToAgent, navigateToChat } = useNavigatePage();
  const { type, runId } = useEvaluationUrl();
  const { visible: configVisible, switchVisible: switchConfigVisible } =
    useSetModalState(true);
  const [rowSelection, setRowSelection] = useState({});

  const { data } = useFetchEvaluationRun();

  const { exportEvaluationRun } = useExportEvaluationRun();

  const evaluationSchema = useEvaluationSchema();

  const form = useForm<EvaluationSettingsFormType>({
    resolver: zodResolver(evaluationSchema),
    defaultValues: {
      config_snapshot: {
        target: {},
        metrics: {},
      },
    },
  });

  const { handleApplyConfig } = useApplyConfig(form);

  const handleBack = useCallback(() => {
    if (type === EvaluationType.Agent) {
      navigateToAgent(id!)();
    } else {
      navigateToChat(id!)();
    }
  }, [type, navigateToAgent, id, navigateToChat]);

  const handleExport = useCallback(() => {
    exportEvaluationRun();
  }, [exportEvaluationRun]);

  return (
    <section className="h-full flex flex-col">
      <PageHeader>
        <Button onClick={handleBack} variant={'ghost'}>
          <ArrowBigLeft /> Back
        </Button>
      </PageHeader>
      <div className="flex flex-1 min-h-0 pb-9 gap-4">
        <EvaluationList selectedRunId={runId} type={type} />

        <section className="flex-1 min-w-0 flex flex-col">
          <section className="flex justify-between pb-4">
            <div className="flex items-center gap-2">
              <span> {data?.name}</span>
              <Tooltip>
                <TooltipTrigger>
                  <Button
                    variant={'ghost'}
                    disabled={!runId}
                    onClick={handleApplyConfig}
                  >
                    <ListCheck />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>{t('chat.applyModelConfigs')}</p>
                </TooltipContent>
              </Tooltip>
            </div>
            <div className="space-x-4">
              <Button
                variant={'ghost'}
                onClick={handleExport}
                disabled={!runId}
              >
                <Upload /> Export
              </Button>
              <RunDropdownButton
                rowSelection={rowSelection}
              ></RunDropdownButton>
            </div>
          </section>
          <EvaluationTable
            rowSelection={rowSelection}
            setRowSelection={setRowSelection}
          />
        </section>
        {configVisible ? (
          <EvaluationConfigPanel
            type={type}
            visible={configVisible}
            onClose={switchConfigVisible}
            form={form}
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
