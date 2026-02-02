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
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useFetchHistoryList } from './hook/billing-history';

const BillingHistory: React.FC = () => {
  const { invoicesData, pagination, setPagination } = useFetchHistoryList();
  const [currentPage, setCurrentPage] = useState(1);
  const itemsPerPage = 10;
  const totalPages = Math.ceil(invoicesData.length / itemsPerPage);
  const { t } = useTranslation();

  const handlePageChange = (page: number) => {
    setCurrentPage(page);
  };

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
    <div>
      <Table>
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
        {/* <Pagination className="justify-end mx-0">
          <PaginationContent>
            <PaginationPrevious
              onClick={() => handlePageChange(currentPage - 1)}
              disabled={currentPage === 1}
            />
            {[...Array(totalPages).keys()].map((page) => (
              <PaginationItem key={page}>
                <PaginationLink
                  isActive={currentPage === page + 1}
                  onClick={() => handlePageChange(page + 1)}
                >
                  {page + 1}
                </PaginationLink>
              </PaginationItem>
            ))}
            <PaginationNext
              onClick={() => handlePageChange(currentPage + 1)}
              disabled={currentPage === totalPages}
            />
          </PaginationContent>
        </Pagination> */}
      </div>
    </div>
  );
};

export default BillingHistory;
