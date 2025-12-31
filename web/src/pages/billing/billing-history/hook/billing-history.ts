import { DateRange } from '@/components/originui/calendar';
import { useGetPaginationWithRouter } from '@/hooks/logic-hooks';
import billingService from '@/services/price';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { ITableInvoice, Invoice } from '../../interface';
const stateMap = {
  paid: 'Success',
  unpaid: 'Failed',
  pending: 'Pending',
};
export const useFetchHistoryList = () => {
  // amount: number;
  //   created_at: number;
  //   currency: string;
  //   hosted_invoice_url: string;
  //   invoice_id: string;
  //   invoice_pdf_url: string;
  //   status: 'paid' | 'unpaid' | 'pending';
  const getDate = (currentDate: DateRange) => {
    const startDate = new Date(currentDate.from);
    startDate.setHours(0, 0, 0, 0);
    const endDate = new Date(currentDate.to || new Date());
    endDate.setHours(23, 59, 59, 999);
    return {
      start: Math.round(startDate.getTime() / 1000),
      end: Math.round(endDate.getTime() / 1000),
    };
  };
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const [invoicesData, setInvoicesData] = useState<ITableInvoice[] | never[]>(
    [],
  );
  const { data, isLoading: loading } = useQuery({
    queryKey: ['fetchHistoryList'],
    // initialData: { docs: [], total: 0 },
    queryFn: async () => {
      const { data } = await billingService.spendHistory();
      console.log('spendHistory-data', data);

      // const data = [
      //   {
      //     invoice_id: 'INV-001',
      //     created_at: 1762876800,
      //     status: 'paid',
      //     amount: 50.0,
      //     invoice_pdf_url: 'https://example.com/invoice-001',
      //   },
      //   {
      //     invoice_id: 'INV-002',
      //     created_at: 1762876800,
      //     status: 'unpaid',
      //     amount: 75.0,
      //     invoice_pdf_url: 'https://example.com/invoice-002',
      //   },
      //   {
      //     invoice_id: 'INV-003',
      //     created_at: 1762876800,
      //     status: 'pending',
      //     amount: 0,
      //     invoice_pdf_url: 'https://example.com/invoice-003',
      //   },
      //   {
      //     invoice_id: 'INV-004',
      //     created_at: 1762876800,
      //     status: 'unpaid',
      //     amount: 0,
      //     invoice_pdf_url: 'https://example.com/invoice-003',
      //   },
      // ] as Invoice[];

      return data.data ?? [];
    },
  });

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
        invoiceLink: item.invoice_pdf_url,
        product: 'Chat2DB',
      } as ITableInvoice;
    });
    setInvoicesData(tempData || []);
  }, [data, setPagination]);

  return { invoicesData, loading, pagination, setPagination };
};
