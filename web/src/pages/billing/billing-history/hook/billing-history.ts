import { useGetPaginationWithRouter } from '@/hooks/logic-hooks';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { ITableInvoice, Invoice } from '../../interface';

export const useFetchHistoryList = () => {
  // amount: number;
  //   created_at: number;
  //   currency: string;
  //   hosted_invoice_url: string;
  //   invoice_id: string;
  //   invoice_pdf_url: string;
  //   status: 'paid' | 'unpaid' | 'pending';
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const [invoicesData, setInvoicesData] = useState<ITableInvoice[] | never[]>(
    [],
  );
  const { data, isLoading: loading } = useQuery({
    queryKey: ['fetchHistoryList'],
    // initialData: { docs: [], total: 0 },
    queryFn: async () => {
      // const { data } = await billingService.listHistory()
      const data = [
        {
          invoice_id: 'INV-001',
          created_at: 1762876800,
          status: 'paid',
          amount: 50.0,
          invoice_pdf_url: 'https://example.com/invoice-001',
        },
        {
          invoice_id: 'INV-002',
          created_at: 1762876800,
          status: 'unpaid',
          amount: 75.0,
          invoice_pdf_url: 'https://example.com/invoice-002',
        },
        {
          invoice_id: 'INV-003',
          created_at: 1762876800,
          status: 'pending',
          amount: 0,
          invoice_pdf_url: 'https://example.com/invoice-003',
        },
        {
          invoice_id: 'INV-004',
          created_at: 1762876800,
          status: 'unpaid',
          amount: 0,
          invoice_pdf_url: 'https://example.com/invoice-003',
        },
      ] as Invoice[];

      return data ?? [];
    },
  });

  const stateMap = {
    paid: 'Success',
    unpaid: 'Failed',
    pending: 'Pending',
  };

  useEffect(() => {
    if (!data || data.length === 0) {
      setPagination({
        total: 0,
        page: 1,
        pageSize: 10,
      });
      setInvoicesData([]);
    }
    const tempData = data?.map((item: Invoice) => {
      return {
        id: item.invoice_id,
        amount: item.amount.toFixed(2),
        status: item.status ? stateMap[item.status] : '-',
        createDate: new Date(item.created_at * 1000).toLocaleDateString(),
        invoiceUrl: item.invoice_pdf_url,
        product: 'Chat2DB',
      } as ITableInvoice;
    });
    setInvoicesData(tempData || []);
  }, [data]);

  return { invoicesData, loading, pagination, setPagination };
};
