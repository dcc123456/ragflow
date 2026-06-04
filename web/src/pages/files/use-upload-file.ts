import { UploadFormSchemaType } from '@/components/file-upload-dialog';
import { useSetModalState } from '@/hooks/common-hooks';
import { useUploadFile } from '@/hooks/use-file-request';
import { showPriceModal } from '@/pages/price/global/hook';
import { useCallback } from 'react';
import { useGetFolderId } from './hooks';

export const useHandleUploadFile = () => {
  const {
    visible: fileUploadVisible,
    hideModal: hideFileUploadModal,
    showModal: showFileUploadModal,
  } = useSetModalState();
  const { uploadFile, loading } = useUploadFile();
  const id = useGetFolderId();

  const onFileUploadOk = useCallback(
    async ({ fileList }: UploadFormSchemaType): Promise<number | undefined> => {
      if (fileList.length > 0) {
        const ret = await uploadFile({ fileList, parentId: id });

        // The upload goes through umi-request (not raw axios), so the
        // global response interceptor in utils/request.ts already shows
        // the upgrade modal for billing codes 2000-2005. We also dispatch
        // the modal explicitly here so codes 2006/2007 are covered and the
        // file-management page matches the dataset upload behavior.
        if (
          ret &&
          typeof ret.code === 'number' &&
          ret.code >= 2000 &&
          ret.code <= 2007
        ) {
          showPriceModal({
            code: ret.code,
            detail: (ret as any).detail,
            message: ret.message,
          });
          return ret.code;
        }

        if (ret?.code === 0) {
          hideFileUploadModal();
          return 0;
        }

        return ret?.code;
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
