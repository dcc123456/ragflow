import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { SquareChartGantt } from 'lucide-react';
import React, { useState } from 'react';

interface Invoice {
  id: string;
  createDate: string;
  product: string;
  status: string;
  amount: string;
  invoiceLink?: string;
}

const invoicesData: Invoice[] = [
  {
    id: 'INV-001',
    createDate: '2023-07-01',
    product: 'Product A',
    status: 'Success',
    amount: '$50.00',
    invoiceLink: 'https://example.com/invoice-001',
  },
  {
    id: 'INV-002',
    createDate: '2023-07-05',
    product: 'Product B',
    status: 'Pending',
    amount: '$75.00',
    invoiceLink: 'https://example.com/invoice-002',
  },
  {
    id: 'INV-003',
    createDate: '2023-07-10',
    product: 'Product C',
    status: 'Success',
    amount: '00.00',
    invoiceLink: 'https://example.com/invoice-003',
  },
  {
    id: 'INV-003',
    createDate: '2023-07-10',
    product: 'Product C',
    status: 'Failed',
    amount: '00.00',
    invoiceLink: 'https://example.com/invoice-003',
  },
];

const BillingHistory: React.FC = () => {
  const [currentPage, setCurrentPage] = useState(1);
  const itemsPerPage = 10;
  const totalPages = Math.ceil(invoicesData.length / itemsPerPage);

  const handlePageChange = (page: number) => {
    setCurrentPage(page);
  };

  const handleStadus = (status: string) => {
    let classname = '';
    switch (status) {
      case 'Success':
        classname = 'bg-green-500';
        break;
      case 'Pending':
        classname = 'bg-sky-500';
        break;
      case 'Failed':
        classname = 'bg-red-500';
        break;
      default:
        return null;
    }
    return (
      <div className="flex items-center gap-1">
        {status}
        <div className={`w-1 h-1 rounded-full ${classname}`}></div>
      </div>
    );
  };
  return (
    <div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Invoice ID</TableHead>
            <TableHead>Create Date</TableHead>
            <TableHead>Product</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Amount</TableHead>
            <TableHead>Invoice</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {invoicesData.map((invoice) => (
            <TableRow key={invoice.id}>
              <TableCell>{invoice.id}</TableCell>
              <TableCell>{invoice.createDate}</TableCell>
              <TableCell>{invoice.product}</TableCell>
              <TableCell>{handleStadus(invoice.status)}</TableCell>
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
        <Pagination className="justify-end mx-0">
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
        </Pagination>
      </div>
    </div>
  );
};

export default BillingHistory;
