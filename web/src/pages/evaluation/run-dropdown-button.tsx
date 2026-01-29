import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useCalculateEvaluationRunsMetrics } from '@/hooks/use-evaluation-request';
import { useEvaluationUrl } from '@/hooks/use-evaluation-url';
import { buildOptions } from '@/utils/form';
import { Play } from 'lucide-react';
import { useTranslation } from 'react-i18next';

enum RunType {
  All = 'all',
  Relevancy = 'relevancy',
  Factuality = 'factuality',
  Consistency = 'consistency',
}

const RunList = [
  RunType.All,
  RunType.Relevancy,
  RunType.Factuality,
  RunType.Consistency,
];

export function RunDropdownButton() {
  const { t } = useTranslation();
  const options = buildOptions(RunList, t, 'evaluation');
  const { runId } = useEvaluationUrl();

  const { calculateEvaluationRunsMetrics } =
    useCalculateEvaluationRunsMetrics();

  const run = (type: RunType) => () => {
    if (type === RunType.All) {
      calculateEvaluationRunsMetrics();
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button disabled={!runId}>
          <Play />
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
