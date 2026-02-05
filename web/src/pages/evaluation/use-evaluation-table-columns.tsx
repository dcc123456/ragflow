import { Checkbox } from '@/components/ui/checkbox';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { useFetchEvaluationRunResults } from '@/hooks/use-evaluation-request';
import { IMetrics, IMetricsSummary } from '@/interfaces/database/evaluation';
import { ColumnDef } from '@tanstack/react-table';
import { get, round } from 'lodash';
import { ArrowUpDown, Eye } from 'lucide-react';
import { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

const CellWithTooltip = ({ content }: { content: ReactNode }) => {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="max-w-[300px] truncate">{content}</div>
        </TooltipTrigger>
        <TooltipContent>
          <p className="max-w-md">{content}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
};

export interface EvaluationResultRow extends Pick<
  IMetrics,
  | 'faithfulness'
  | 'faithfulness_reason'
  | 'semantic_similarity'
  | 'semantic_similarity_reason'
  | 'context_relevance'
  | 'context_relevance_reason'
> {
  id: string;
  question: string;
  referenceAnswer?: string;
  modelAnswer?: string;
}

export interface EvaluationTableColumnsProps {
  onShowDetail?: (caseId: string) => void;
}

function MetricHeader({
  column,
  name,
  metricsSummary,
}: {
  column: any;
  metricsSummary?: IMetricsSummary;
  name: string;
}) {
  const { t } = useTranslation();
  return (
    <div
      className="flex items-center gap-2 cursor-pointer"
      onClick={() => column.toggleSorting(column.getIsSorted() === 'asc')}
    >
      {t('evaluation.relevancy')}
      <span className="text-accent-primary text-xs">
        {round(get(metricsSummary, `${name}.summary`, 0), 2)}
      </span>
      <ArrowUpDown className="h-4 w-4" />
    </div>
  );
}

export const useEvaluationTableColumns = (
  props?: EvaluationTableColumnsProps,
) => {
  const { t } = useTranslation();
  const { data: results } = useFetchEvaluationRunResults();
  const { onShowDetail } = props || {};

  const metricsSummary = results.run?.metrics_summary;

  const columns: ColumnDef<EvaluationResultRow>[] = [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={
            table.getIsAllPageRowsSelected() ||
            (table.getIsSomePageRowsSelected() && 'indeterminate')
          }
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label="Select all"
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label="Select row"
        />
      ),
      enableSorting: false,
      enableHiding: false,
      size: 50,
    },
    {
      accessorKey: 'question',
      header: t('evaluation.question'),
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          <CellWithTooltip content={row.original.question} />
          <Eye
            className="size-4 cursor-pointer hover:text-accent-primary opacity-0 group-hover:opacity-100 transition-opacity"
            onClick={() => onShowDetail?.(row.original.id)}
          />
        </div>
      ),
    },
    {
      accessorKey: 'referenceAnswer',
      header: t('evaluation.referenceAnswer'),
      cell: ({ row }) => (
        <CellWithTooltip content={row.original.referenceAnswer || '-'} />
      ),
    },
    {
      accessorKey: 'modelAnswer',
      header: t('evaluation.modelAnswer'),
      minSize: 200,
      cell: ({ row }) => (
        <CellWithTooltip content={row.original.modelAnswer || '-'} />
      ),
    },
    {
      accessorKey: 'context_relevance',
      size: 150,
      header: ({ column }) => {
        return (
          <MetricHeader
            column={column}
            name="context_relevance"
            metricsSummary={metricsSummary}
          />
        );
      },
      cell: ({ row }) => {
        const relevancy = row.original.context_relevance;

        return (
          <div className="text-left">
            <div>{relevancy !== undefined ? relevancy.toFixed(2) : '-'}</div>
          </div>
        );
      },
    },
    {
      accessorKey: 'faithfulness',
      size: 150,
      header: ({ column }) => {
        return (
          <MetricHeader
            column={column}
            name="faithfulness"
            metricsSummary={metricsSummary}
          />
        );
      },
      cell: ({ row }) => {
        const factuality = row.original.faithfulness;

        return (
          <div className="text-left">
            <div>{factuality !== undefined ? factuality.toFixed(2) : '-'}</div>
          </div>
        );
      },
    },
    {
      accessorKey: 'semantic_similarity',
      size: 150,
      header: ({ column }) => {
        return (
          <MetricHeader
            column={column}
            name="semantic_similarity"
            metricsSummary={metricsSummary}
          />
        );
      },
      cell: ({ row }) => {
        const consistency = row.original.semantic_similarity;

        return (
          <div className="text-left">
            <div>
              {typeof consistency === 'number' ? consistency.toFixed(2) : '-'}
            </div>
          </div>
        );
      },
    },
  ];

  return columns;
};
