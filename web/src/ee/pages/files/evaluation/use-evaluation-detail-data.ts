import message from '@/components/ui/message';
import {
  useFetchEvaluationCollection,
  useFetchEvaluationFileContent,
} from '@/ee/hooks/use-evaluation-request';
import evaluationService, {
  evaluationExportBlob,
} from '@/ee/services/evaluation-service';
import { downloadFileFromBlob } from '@/utils/file-util';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import { EvaluationFileApiAction } from '@/ee/pages/files/evaluation/constant';

export function useFetchEvaluationDetail() {
  const { id } = useParams();
  const { data, loading } = useFetchEvaluationCollection(id as string);

  return {
    data,
    loading,
  };
}

export function useFetchEvaluationDetailList() {
  const { id } = useParams();
  const { data, loading, pagination, setPagination, onPageChange } =
    useFetchEvaluationFileContent(id as string);

  return {
    data,
    pagination,
    setPagination,
    onPageChange,
    loading,
  };
}

async function fetchDocumentBlob(id: string) {
  const response = await evaluationService.downloadEvaluationFile(id);
  return evaluationExportBlob(response.data);
}

export const downloadDocument = async ({
  id,
  filename,
}: {
  id: string;
  filename?: string;
}) => {
  const blob = await fetchDocumentBlob(id);
  downloadFileFromBlob(blob, filename);
};

export const useDownloadDocument = () => {
  const { id } = useParams();

  const { t } = useTranslation();

  const { mutateAsync } = useMutation({
    mutationKey: [EvaluationFileApiAction.DownloadDocument, id],
    onError: (error: Error) => message.error(error.message),

    mutationFn: async () => {
      await downloadDocument({ id: id as string });
      message.success(t('message.modified'));
    },
  });

  return {
    download: mutateAsync,
  };
};
