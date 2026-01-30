import { UploadFormSchemaType } from '@/components/file-upload-dialog';
import message from '@/components/ui/message';
import { useSetModalState } from '@/hooks/common-hooks';
import fileManagerService from '@/services/file-manager-service';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useGetFolderId } from '../hooks';
import { EvaluationFileApiAction } from './constant';

export const useUploadEvaluationFile = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [EvaluationFileApiAction.UploadFile],
    mutationFn: async (params: { fileList: File[] }) => {
      const fileList = params.fileList;
      const formData = new FormData();
      fileList.forEach((file: any) => {
        formData.append('file', file);
      });
      try {
        const ret = await fileManagerService.uploadEvaluationFile(formData);
        if (ret?.data.code === 0) {
          message.success(t('message.uploaded'));
          queryClient.invalidateQueries({
            queryKey: [EvaluationFileApiAction.FetchEvaluationList],
          });
        }
        return ret?.data?.code;
      } catch (error) {}
    },
  });

  return { data, loading, uploadFile: mutateAsync };
};

export const useHandleUploadEvaluationFile = () => {
  const {
    visible: fileUploadVisible,
    hideModal: hideFileUploadModal,
    showModal: showFileUploadModal,
  } = useSetModalState();
  const { uploadFile, loading } = useUploadEvaluationFile();
  const id = useGetFolderId();

  const onFileUploadOk = useCallback(
    async ({ fileList }: UploadFormSchemaType): Promise<number | undefined> => {
      if (fileList.length > 0) {
        const ret: number = await uploadFile({ fileList });
        if (ret === 0) {
          hideFileUploadModal();
        }
        return ret;
      }
    },
    [uploadFile, hideFileUploadModal, id],
  );

  return {
    fileUploadLoading: loading,
    onFileUploadOk,
    fileUploadVisible,
    hideFileUploadModal,
    showFileUploadModal,
  };
};
