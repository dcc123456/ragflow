import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import ProcessLogModal from '@/pages/dataset/process-log-modal';
import type { ILogInfo } from '@/pages/dataset/process-log-modal';
import { RunningStatus } from '@/constants/knowledge';
import { formatDate, formatSecondsToHumanReadable } from '@/utils/date';
import { formatBytes } from '@/utils/file-util';

import { useIngestionLog, useIngestionLogMessages } from './hooks';

const OperationStatusToRunningStatus: Record<string, RunningStatus> = {
  '0': RunningStatus.UNSTART,
  '1': RunningStatus.RUNNING,
  '2': RunningStatus.CANCEL,
  '3': RunningStatus.DONE,
  '4': RunningStatus.FAIL,
  '5': RunningStatus.SCHEDULE,
};

export function IngestionLogModal({
  logId,
  visible,
  onClose,
}: {
  logId: string;
  visible: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { data: detail } = useIngestionLog(logId, visible);
  const {
    data: messages,
    fetchPreviousPage,
    hasPreviousPage,
    isFetchingPreviousPage,
  } = useIngestionLogMessages(logId, visible);

  const logInfo = useMemo<ILogInfo>(() => {
    const events = messages?.items.length ? messages.items : undefined;
    return {
      taskId: detail?.document_id,
      fileName: detail?.name || '-',
      fileType: detail?.suffix,
      uploadedBy: detail?.nickname,
      uploadDate: formatDate(detail?.create_date),
      fileSize: detail?.size != null ? formatBytes(detail.size) : undefined,
      processBeginAt: formatDate(detail?.process_begin_at),
      chunkNumber: detail?.chunk_count ?? undefined,
      duration:
        detail?.process_duration != null
          ? formatSecondsToHumanReadable(detail.process_duration)
          : undefined,
      status: detail
        ? (OperationStatusToRunningStatus[detail.operation_status] ??
          RunningStatus.UNSTART)
        : undefined,
      details: events ? '' : '-',
      events,
      loadPreviousEvents: hasPreviousPage
        ? () => fetchPreviousPage()
        : undefined,
      hasPreviousEvents: hasPreviousPage,
      isLoadingPreviousEvents: isFetchingPreviousPage,
    };
  }, [
    detail,
    messages,
    fetchPreviousPage,
    hasPreviousPage,
    isFetchingPreviousPage,
  ]);

  return (
    <ProcessLogModal
      title={t('admin.ingestionTasks.logTitle')}
      visible={visible}
      onCancel={onClose}
      logInfo={logInfo}
    />
  );
}
