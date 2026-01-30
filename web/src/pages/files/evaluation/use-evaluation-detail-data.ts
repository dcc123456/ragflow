import message from '@/components/ui/message';
import { FileMimeType } from '@/constants/common';
import {
  useFetchEvaluationCollection,
  useFetchEvaluationFileContent,
} from '@/hooks/use-evaluation-request';
import evaluationService from '@/services/evaluation-service';
import api from '@/utils/api';
import { downloadFileFromBlob } from '@/utils/file-util';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import { EvaluationFileApiAction } from './constant';

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

async function fetchDocumentBlob(id: string, mimeType?: FileMimeType) {
  console.log('fetchDocumentBlob', id);
  const response = await evaluationService.downloadEvaluationFile(
    {
      url: api.downloadEvaluationFile(id),
      method: 'get',
      responseType: 'blob',
    },
    true,
  );
  const blob = new Blob([response.data], {
    type: mimeType || response.data.type,
  });

  return blob;
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

    mutationFn: async () => {
      console.log('downloadDocument', id);
      await downloadDocument({ id: id as string });
      message.success(t('message.modified'));
    },
  });

  return {
    download: mutateAsync,
  };
};
