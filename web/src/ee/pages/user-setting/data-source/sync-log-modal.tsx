import { listSyncLogEvents } from '@/ee/services/data-source-service';
import type {
  IngestionMessageParams,
  IngestionMessagesResponse,
} from '@/interfaces/database/ingestion';
import { useLogMessages } from '@/pages/dataset/ingestion-message-hooks';
import ProcessLogModal from '@/pages/dataset/process-log-modal';
import type { IDataSourceLog } from '@/pages/user-setting/data-source/interface';
import { getSummary } from '@/pages/user-setting/data-source/log-summary';
import { formatDate } from '@/utils/date';
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

const SyncLogKeys = {
  events: (id: string) => ['syncLogEvents', id] as const,
};

export function SyncLogModal({
  log,
  onClose,
}: {
  log: IDataSourceLog;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const queryKey = useMemo(() => SyncLogKeys.events(log.id), [log.id]);
  const loadEvents = useCallback(
    async (params: IngestionMessageParams) => {
      const { data } = await listSyncLogEvents(log.id, params);
      const page = data?.data as
        | Omit<IngestionMessagesResponse, 'run_count'>
        | undefined;
      if (!page || !Array.isArray(page.items)) {
        throw new Error('Sync log events are unavailable');
      }
      return page;
    },
    [log.id],
  );
  const { data, fetchPreviousPage, hasPreviousPage, isFetchingPreviousPage } =
    useLogMessages(queryKey, loadEvents, true);
  // The live countdown stays in the table. The modal only shows text summaries.
  const summary = getSummary(log, 0);
  const events = data?.items.length ? data.items : undefined;

  return (
    <ProcessLogModal
      visible
      title={t('knowledgeDetails.syncLog')}
      onCancel={onClose}
      detailsLabel={events ? t('knowledgeDetails.summary') : undefined}
      logInfo={{
        taskId: log.id,
        dataset: log.kb_name,
        task: log.task_type || 'sync',
        status: log.status,
        startDate: log.time_started ? formatDate(log.time_started) : undefined,
        details: log.error_msg || (typeof summary === 'string' ? summary : ''),
        events,
        loadPreviousEvents: hasPreviousPage ? fetchPreviousPage : undefined,
        hasPreviousEvents: hasPreviousPage,
        isLoadingPreviousEvents: isFetchingPreviousPage,
      }}
    />
  );
}
