import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { pick } from 'lodash';
import { SquareChartGantt } from 'lucide-react';
import React from 'react';
import { useTranslation } from 'react-i18next';
import { useFetchHistoryList } from './hook/billing-history';

const BillingHistory: React.FC = () => {
  const { invoicesData, pagination, setPagination } = useFetchHistoryList();
  const { t } = useTranslation();

  const handleStatus = (status: string) => {
    let classname = '';
    let displayStatus = status;
    switch (status) {
      case 'Success':
        classname = 'bg-green-500';
        displayStatus = t('billing.success');
        break;
      case 'Pending':
        classname = 'bg-sky-500';
        displayStatus = t('billing.pending');
        break;
      case 'Failed':
        classname = 'bg-red-500';
        displayStatus = t('billing.failed');
        break;
      default:
        return null;
    }
    return (
      <div className="flex items-center gap-1">
        {displayStatus}
        <div className={`w-1 h-1 rounded-full ${classname}`}></div>
      </div>
    );
  };
  return (
    <div className="flex flex-col h-full">
      <Table className="flex-1 overflow-auto">
        <TableHeader>
          <TableRow>
            <TableHead>{t('billing.invoiceID')}</TableHead>
            <TableHead>{t('billing.createDate')}</TableHead>
            <TableHead>{t('billing.product')}</TableHead>
            <TableHead>{t('billing.status')}</TableHead>
            <TableHead>{t('billing.amount')}</TableHead>
            <TableHead>{t('billing.invoice')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {invoicesData.map((invoice) => (
            <TableRow key={invoice.id}>
              <TableCell>{invoice.id}</TableCell>
              <TableCell>{invoice.createDate}</TableCell>
              <TableCell>{invoice.product}</TableCell>
              <TableCell>{handleStatus(invoice.status)}</TableCell>
              <TableCell>{invoice.amount}</TableCell>
              <TableCell>
                {invoice.invoiceLink && (
                  <a
                    href={invoice.invoiceLink}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {/* icon */}
                    <SquareChartGantt />
                  </a>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <div className="flex justify-end items-center mt-4 w-full">
        <RAGFlowPagination
          {...pick(pagination, 'current', 'pageSize')}
          total={pagination.total}
          onChange={(page, pageSize) => {
            setPagination({ page, pageSize });
          }}
        ></RAGFlowPagination>
      </div>
    </div>
  );
};

export default BillingHistory;
