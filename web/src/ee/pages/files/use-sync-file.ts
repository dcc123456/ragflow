import { useSetModalState } from '@/hooks/common-hooks';
import {
  DataSourceFileItem,
  syncDataSourceFiles,
} from '@/services/data-source-service';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';

export const useHandleSyncFile = () => {
  const {
    visible: syncFileVisible,
    hideModal: hideSyncFileModal,
    showModal: showSyncFileModal,
  } = useSetModalState();

  const { t } = useTranslation();

  const { mutateAsync, isPending } = useMutation({
    mutationFn: async (params: {
      dataSource: string;
      syncFiles: DataSourceFileItem[];
      targetFolder: string;
    }) => {
      const { data: ret } = await syncDataSourceFiles(
        params.dataSource,
        params.targetFolder,
        params.syncFiles,
      );

      if (ret?.code === 0) {
        toast.success(t('fileManager.syncFileSuccess'));
        hideSyncFileModal?.();
        return true;
      } else {
        toast.error(ret?.message);
        return false;
      }
    },
  });

  return {
    syncFileVisible,
    hideSyncFileModal,
    showSyncFileModal,
    syncFileLoading: isPending,
    syncFile: mutateAsync,
  };
};
