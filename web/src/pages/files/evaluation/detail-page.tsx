import BackButton from '@/components/back-button';
import { EmptyType } from '@/components/empty/constant';
import Empty from '@/components/empty/empty';
import { Button } from '@/components/ui/button';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { Upload } from 'lucide-react';
import { FC } from 'react';
import { useTranslation } from 'react-i18next';
import { FileTabs } from '..';
import {
  useDownloadDocument,
  useFetchEvaluationDetail,
  useFetchEvaluationDetailList,
} from './use-evaluation-detail-data';

interface DetailPageProps {
  name: string;
  pagination: {
    current: number;
    pageSize: number;
    total: number;
  };
  onPageChange: (page: number, pageSize: number) => void;
  onBack: () => void;
}

export const DetailPage: FC<DetailPageProps> = ({}) => {
  const { t } = useTranslation();
  const { navigateToFileManagerEvaluation } = useNavigatePage();
  const { data, pagination, onPageChange } = useFetchEvaluationDetailList();
  const { data: detail } = useFetchEvaluationDetail();
  const { download: downloadDocument } = useDownloadDocument();
  const onBack = () => {
    navigateToFileManagerEvaluation(FileTabs.EVALUATION);
  };

  return (
    <div className="p-6 bg-background min-h-screen">
      <div className="flex items-center mb-6">
        <BackButton onClick={onBack} />
      </div>
      <div className="w-full flex justify-between items-center pb-5">
        <h2 className="text-lg font-semibold flex items-center">
          {detail?.name || ''}
        </h2>
        <Button
          variant={'outline'}
          className="text-text-sm"
          onClick={() => {
            downloadDocument();
          }}
        >
          <Upload />
          {t('fileManager.evaluation.export')}
        </Button>
      </div>

      <div className="rounded-lg w-full">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-1/2">
                {t('fileManager.evaluation.query')}
              </TableHead>
              <TableHead className="w-1/2">
                {t('fileManager.evaluation.answer')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data?.cases?.length > 0 &&
              data.cases.map((item, index) => (
                <TableRow key={index}>
                  <TableCell className="text-sm">
                    {item.variable.question}
                  </TableCell>
                  <TableCell className="text-sm">
                    {item.variable.reference_answer}
                  </TableCell>
                </TableRow>
              ))}
            {!data?.cases?.length && (
              <TableRow>
                <TableCell colSpan={2} className="h-24 text-center">
                  <Empty
                    type={EmptyType.Data}
                    text={t('fileManager.evaluation.noEvaluationData')}
                  />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex justify-end mt-4">
        <RAGFlowPagination
          current={pagination?.current || 0}
          pageSize={pagination?.pageSize || 10}
          total={pagination?.total || 1}
          onChange={(page, pageSize) => onPageChange({ page, pageSize })}
        />
      </div>
    </div>
  );
};

export default DetailPage;
