import { useGetPaginationWithRouter } from '@/hooks/logic-hooks';
import billingService from '@/services/price';
import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';
import { BillingQueryKey } from '../../constants/query-keys';
import { ITableInvoice, Invoice } from '../../interface';

const stateMap = {
  paid: 'Success',
  unpaid: 'Failed',
  pending: 'Pending',
};

export const useFetchHistoryList = () => {
  const { pagination, setPagination } = useGetPaginationWithRouter();

  const { data, isLoading: loading } = useQuery<{
    total: number;
    items: Invoice[];
  }>({
    queryKey: [BillingQueryKey.HistoryList, pagination],
    queryFn: async () => {
      const { data } = await billingService.spendHistory({
        page: pagination.current,
        page_size: pagination.pageSize,
      });

      return data.data ?? { total: 0, items: [] };
    },
  });

  const invoicesData: ITableInvoice[] = useMemo(() => {
    const items = data?.items ?? [];
    return items.map((item: Invoice) => ({
      id: item.invoice_id,
      amount: item.amount.toFixed(2),
      status: item.status ? stateMap[item.status] : '-',
      createDate: new Date(item.created_at * 1000).toLocaleDateString(),
      invoiceLink: item.invoice_pdf_url,
      product: item.product || 'UNKNOWN',
    }));
  }, [data]);

  return {
    invoicesData,
    loading,
    pagination: { ...pagination, total: data?.total ?? 0 },
    setPagination,
  };
};
