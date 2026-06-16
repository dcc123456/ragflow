import { UploadFormSchemaType } from '@/components/file-upload-dialog';
import { useSetModalState } from '@/hooks/common-hooks';
import {
  useRunDocument,
  useUploadDocument,
} from '@/hooks/use-document-request';
import { showPriceModal } from '@/pages/price/global/hook';
import { getUnSupportedFilesCount } from '@/utils/document-util';
import { useCallback } from 'react';

export const useHandleUploadDocument = () => {
  const {
    visible: documentUploadVisible,
    hideModal: hideDocumentUploadModal,
    showModal: showDocumentUploadModal,
  } = useSetModalState();
  const { uploadDocument, loading } = useUploadDocument();
  const { runDocumentByIds } = useRunDocument();

  const onDocumentUploadOk = useCallback(
    async ({
      fileList,
      parseOnCreation,
      tableColumnMode,
      tableColumnRoles,
    }: UploadFormSchemaType) => {
      if (fileList.length > 0) {
        // Build parser_config if column roles are configured
        let parserConfig: Record<string, any> | undefined;
        if (
          tableColumnMode === 'manual' &&
          tableColumnRoles &&
          Object.keys(tableColumnRoles).length > 0
        ) {
          parserConfig = {
            table_column_mode: 'manual',
            table_column_roles: tableColumnRoles,
          };
        }

        const ret = await uploadDocument(fileList as File[], parserConfig);

        // Check for success (code === 0) or partial success (code === 500 with some files)
        const isSuccess = ret?.code === 0;
        const isPartialSuccess = ret?.code === 500 && ret?.message;

        if (!isSuccess && !isPartialSuccess) {
          // The upload service uses raw axios (see knowledge-service.ts:347),
          // which bypasses the global request interceptor that would otherwise
          // dispatch the UpgradeTipsModal for billing codes. We must handle the
          // display here for codes 2000-2007:
          //   - 2000-2004, 2007 -> centered UpgradeTipsModal
          //   - 2005 -> SubscriptionInvalid, also UpgradeTipsModal
          if (
            typeof ret?.code === 'number' &&
            ret.code >= 2000 &&
            ret.code <= 2007
          ) {
            showPriceModal({
              code: ret.code,
              detail: (ret as any).detail,
              message: ret.message,
            });
          }
          return;
        }

        if (isSuccess && parseOnCreation) {
          runDocumentByIds({
            documentIds: ret.data.map((x: any) => x.id),
            run: 1,
          });
        }

        if (isSuccess) {
          hideDocumentUploadModal();
          return 0;
        }

        // For partial success (code 500), check if any files were uploaded
        const count = getUnSupportedFilesCount(ret?.message);
        if (count !== fileList.length) {
          hideDocumentUploadModal();
          return 0;
        }

        return ret?.code;
      }
    },
    [uploadDocument, runDocumentByIds, hideDocumentUploadModal],
  );

  return {
    documentUploadLoading: loading,
    onDocumentUploadOk,
    documentUploadVisible,
    hideDocumentUploadModal,
    showDocumentUploadModal,
  };
};
