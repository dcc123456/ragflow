import { Button } from '@/components/ui/button';
import type { IDataSourceLog } from '@/pages/user-setting/data-source/interface';
import type { ColumnDef } from '@tanstack/react-table';
import { t } from 'i18next';
import { Eye } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SyncLogModal } from './sync-log-modal';

function SyncLogAction({ log }: { log: IDataSourceLog }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const handleOpen = () => setOpen(true);
  const handleClose = () => setOpen(false);

  return (
    <>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t('knowledgeDetails.syncLog')}
        onClick={handleOpen}
      >
        <Eye />
      </Button>
      {open && <SyncLogModal log={log} onClose={handleClose} />}
    </>
  );
}

export const DataSourceLogExtraColumns: ColumnDef<IDataSourceLog>[] = [
  {
    id: 'operations',
    header: () => t('knowledgeDetails.operations'),
    meta: { headerClassName: 'w-24' },
    cell: ({ row }) => <SyncLogAction log={row.original} />,
  },
];
