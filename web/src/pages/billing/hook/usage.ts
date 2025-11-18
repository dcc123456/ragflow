import { DateRange } from '@/components/originui/calendar';
import billingService from '@/services/price';
import { formatPureDate } from '@/utils/date';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import {
  IDeepDocSpendLineChart,
  IEmbeddingSpendLineChart,
  ITotalSpendLineChart,
  Invoice,
} from '../interface';
const totalSpendLineChart1 = {
  data: [],
  title: 'Total Spend',
  value: 0,
};

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
export const useAllSpends = (currentDate: DateRange, force?: boolean) => {
  // const { data: tenantInfo } = useFetchTenantInfo();
  //   const tenantId = tenantInfo?.tenant_id;
  const { start, end } = getDate(currentDate);
  const [totalSpendLineChart, setTotalSpendLineChart] =
    useState<ITotalSpendLineChart>(totalSpendLineChart1);
  const { data, isFetching: loading } = useQuery<Invoice[]>({
    queryKey: ['getAllSpends', currentDate],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.planSpendOverview({
        start,
        end,
      });
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  useEffect(() => {
    let totalSpendLineChartData: ITotalSpendLineChart = {
      data: [],
      title: 'Total Spend',
      value: 0,
    };
    if (data) {
      data.forEach((item) => {
        totalSpendLineChartData.data.push({
          name: formatPureDate(item.created_at * 1000),
          spend: item.amount,
        });
        totalSpendLineChartData.value += item.amount;
      });
    }
    setTotalSpendLineChart(totalSpendLineChartData);
  }, [data]);

  return { totalSpendLineChart, loading };
};

export const useDeepDocSpends = (currentDate: DateRange, force?: boolean) => {
  const deepDocBarChart1 = {
    data: [
      { name: 'Apr 5', pages: 10, spend: 12.85 },
      { name: 'Apr 10', pages: 15, spend: 12.85 },
      { name: 'Apr 15', pages: 8, spend: 12.85 },
      { name: 'Apr 20', pages: 12, spend: 12.85 },
      { name: 'Apr 25', pages: 14, spend: 12.85 },
      { name: 'Apr 26', pages: 14, spend: 12.85 },
      { name: 'Apr 27', pages: 14, spend: 12.85 },
    ],
    value: 12.85,
    pages: 999,
  } as IDeepDocSpendLineChart;
  const [deepDocBarChart, setDeepDocBarChart] =
    useState<IDeepDocSpendLineChart>(deepDocBarChart1);
  const { start, end } = getDate(currentDate);
  const { data, isFetching: loading } = useQuery<Invoice[]>({
    queryKey: ['getDeepDocSpends', currentDate],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.planSpendOverview({
        start,
        end,
      });
      console.log('spendData', data, res);
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  useEffect(() => {
    let deepDocBarChart: IDeepDocSpendLineChart = {
      data: [],
      pages: 0,
      value: 0,
    };
    if (data) {
      data.forEach((item) => {
        deepDocBarChart.data.push({
          name: formatPureDate(item.created_at * 1000),
          pages: item.pages || 0,
          spend: item.amount,
        });
        deepDocBarChart.value += item.amount;
      });
    }
    setDeepDocBarChart(deepDocBarChart);
  }, [data]);

  return { deepDocBarChart, loading };
};

export const useEmbeddingSpends = (currentDate: DateRange, force?: boolean) => {
  const embeddingBarChart1 = {
    data: [
      { name: 'Apr 5', tokens: 10, spend: 12.85 },
      { name: 'Apr 10', tokens: 15, spend: 12.85 },
      { name: 'Apr 15', tokens: 8, spend: 12.85 },
      { name: 'Apr 20', tokens: 12, spend: 12.85 },
      { name: 'Apr 25', tokens: 14, spend: 12.85 },
      { name: 'Apr 26', tokens: 14, spend: 12.85 },
      { name: 'Apr 27', tokens: 14, spend: 12.85 },
    ],
    value: 12.85,
    tokens: 999,
  } as IEmbeddingSpendLineChart;
  const [embeddingBarChart, setEmbeddingBarChart] =
    useState<IEmbeddingSpendLineChart>(embeddingBarChart1);
  const { start, end } = getDate(currentDate);
  const { data, isFetching: loading } = useQuery<Invoice[]>({
    queryKey: ['getEmbeddingSpends', currentDate],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.planSpendOverview({
        start,
        end,
      });
      console.log('spendData', data, res);
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  useEffect(() => {
    let embeddingBarChart: IEmbeddingSpendLineChart = {
      data: [],
      tokens: 0,
      value: 0,
    };
    if (data) {
      data.forEach((item) => {
        embeddingBarChart.data.push({
          name: formatPureDate(item.created_at * 1000),
          tokens: item.tokens || 0,
          spend: item.amount,
        });
        embeddingBarChart.value += item.amount;
      });
    }
    setEmbeddingBarChart(embeddingBarChart);
  }, [data]);

  return { embeddingBarChart, loading };
};
