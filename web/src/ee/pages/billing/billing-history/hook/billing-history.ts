import { useGetPaginationWithRouter } from '@/ee/hooks/use-pagination';
import billingService from '@/ee/services/price';
import { formatIsoDateTime } from '@/utils/date';
import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';
import { BillingQueryKey } from '@/ee/pages/billing/constants/query-keys';
import { ITableInvoice, Invoice } from '@/ee/pages/billing/interface';

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
      createDate: formatIsoDateTime(item.created_at),
      invoiceLink: item.invoice_pdf_url,
      product: item.product || 'UNKNOWN',
      product_quantities: item.product_quantities,
      product_amount_cents: item.product_amount_cents,
    }));
  }, [data]);

  return {
    invoicesData,
    loading,
    pagination: { ...pagination, total: data?.total ?? 0 },
    setPagination,
  };
};
