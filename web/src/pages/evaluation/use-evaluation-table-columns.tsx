import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { ColumnDef } from '@tanstack/react-table';
import { ArrowUpDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export interface EvaluationResultRow {
  caseId: string;
  question: string;
  referenceAnswer?: string;
  modelAnswer?: string;
  relevancy?: number;
  factuality?: number;
  consistency?: number;
  status?: string;
}

export const useEvaluationTableColumns = () => {
  const { t } = useTranslation();

  const calculateAverage = (
    data: EvaluationResultRow[],
    key: keyof EvaluationResultRow,
  ): string => {
    const values = data
      .map((row) => row[key])
      .filter(
        (val): val is number => val !== undefined && typeof val === 'number',
      );

    if (values.length === 0) return '-';
    const avg = values.reduce((sum, val) => sum + val, 0) / values.length;
    return `Avg: ${avg.toFixed(2)}`;
  };

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
    },
    {
      accessorKey: 'question',
      header: t('evaluation.question'),
      cell: ({ row }) => {
        return (
          <div className="max-w-[300px] truncate" title={row.original.question}>
            {row.original.question}
          </div>
        );
      },
    },
    {
      accessorKey: 'referenceAnswer',
      header: t('evaluation.referenceAnswer'),
      cell: ({ row }) => {
        return (
          <div
            className="max-w-[300px] truncate"
            title={row.original.referenceAnswer}
          >
            {row.original.referenceAnswer || '-'}
          </div>
        );
      },
    },
    {
      accessorKey: 'modelAnswer',
      header: t('evaluation.modelAnswer'),
      cell: ({ row }) => {
        return (
          <div
            className="max-w-[300px] truncate"
            title={row.original.modelAnswer}
          >
            {row.original.modelAnswer || '-'}
          </div>
        );
      },
    },
    {
      accessorKey: 'relevancy',
      header: ({ column }) => {
        return (
          <Button
            variant="ghost"
            onClick={() => column.toggleSorting(column.getIsSorted() === 'asc')}
          >
            {t('evaluation.relevancy')}
            <ArrowUpDown className="ml-2 h-4 w-4" />
          </Button>
        );
      },
      cell: ({ row, table }) => {
        const relevancy = row.original.relevancy;
        const avgScore = calculateAverage(
          table.getRowModel().rows.map((r) => r.original),
          'relevancy',
        );

        return (
          <div className="text-center">
            <div>{relevancy !== undefined ? relevancy.toFixed(2) : '-'}</div>
            <div className="text-xs text-text-secondary">{avgScore}</div>
          </div>
        );
      },
    },
    {
      accessorKey: 'factuality',
      header: ({ column }) => {
        return (
          <Button
            variant="ghost"
            onClick={() => column.toggleSorting(column.getIsSorted() === 'asc')}
          >
            {t('evaluation.factuality')}
            <ArrowUpDown className="ml-2 h-4 w-4" />
          </Button>
        );
      },
      cell: ({ row, table }) => {
        const factuality = row.original.factuality;
        const avgScore = calculateAverage(
          table.getRowModel().rows.map((r) => r.original),
          'factuality',
        );

        return (
          <div className="text-center">
            <div>{factuality !== undefined ? factuality.toFixed(2) : '-'}</div>
            <div className="text-xs text-text-secondary">{avgScore}</div>
          </div>
        );
      },
    },
    {
      accessorKey: 'consistency',
      header: ({ column }) => {
        return (
          <Button
            variant="ghost"
            onClick={() => column.toggleSorting(column.getIsSorted() === 'asc')}
          >
            {t('evaluation.consistency')}
            <ArrowUpDown className="ml-2 h-4 w-4" />
          </Button>
        );
      },
      cell: ({ row, table }) => {
        const consistency = row.original.consistency;
        const avgScore = calculateAverage(
          table.getRowModel().rows.map((r) => r.original),
          'consistency',
        );

        return (
          <div className="text-center">
            <div>
              {consistency !== undefined ? consistency.toFixed(2) : '-'}
            </div>
            <div className="text-xs text-text-secondary">{avgScore}</div>
          </div>
        );
      },
    },
  ];

  return columns;
};
