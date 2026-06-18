import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  useCancelEvaluationRun,
  useFetchEvaluationRunResults,
  useStartEvaluationRun,
} from '@/hooks/use-evaluation-request';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { buildOptions } from '@/utils/form';
import { CirclePause, Play } from 'lucide-react';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { NewEvaluationRunId, RunningStatus, RunType } from './constants';

const RunList = [
  RunType.All,
  RunType.Relevancy,
  RunType.Factuality,
  RunType.Consistency,
];

type RunDropdownButtonProps = {
  rowSelection: Record<string, boolean>;
};
export function RunDropdownButton({ rowSelection }: RunDropdownButtonProps) {
  const { t } = useTranslation();
  const options = buildOptions(RunList, t, 'evaluation', true);
  const { runId } = useEvaluationUrl();

  const { startEvaluationRun } = useStartEvaluationRun();

  const { cancelEvaluationRun } = useCancelEvaluationRun();
  const { data: result } = useFetchEvaluationRunResults();
  // const { data } = useFetchEvaluationRun();

  const isRunning = result.run?.status === RunningStatus.RUNNING;

  const run = (type: RunType) => () => {
    const caseIds = Object.keys(rowSelection).filter((id) => rowSelection[id]);

    if (type === RunType.All) {
      startEvaluationRun({ case_ids: caseIds });
    } else {
      startEvaluationRun({ metrics_name: [type], case_ids: caseIds });
    }
  };

  const cancel = useCallback(() => {
    cancelEvaluationRun();
  }, [cancelEvaluationRun]);

  const disabled = !runId || runId === NewEvaluationRunId;

  if (isRunning) {
    return (
      <Button disabled={disabled} onClick={cancel}>
        <CirclePause /> Cancel
      </Button>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button disabled={disabled}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
            <polygon points="20,12 2,3 2,21" />
          </svg>
          Run
        </Button>
      </DropdownMenuTrigger>

      <DropdownMenuContent>
        <DropdownMenuGroup>
          {options.map((option) => (
            <DropdownMenuItem
              key={option.value}
              className="justify-start"
              onClick={run(option.value)}
            >
              <Play /> {option.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
