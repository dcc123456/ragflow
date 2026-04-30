import { DateRange } from '@/components/originui/calendar';
import billingService from '@/services/price';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { BillingQueryKey } from '../constants/query-keys';
import {
  ICategoriesChart,
  ITotalSpendLineChart,
  UsageData,
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

  const [categoriesChart, setCategoriesChart] = useState<ICategoriesChart[]>([
    {
      title: '',
      desc: '',
      series: [],
    },
  ]);
  const { data, isFetching: loading } = useQuery<UsageData>({
    queryKey: [BillingQueryKey.AllSpends, currentDate],
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
      data.series.forEach((item) => {
        totalSpendLineChartData.data.push({
          name: item.date,
          spend: item.spend,
        });
      });
      totalSpendLineChartData.value = data.total_spend;
    }
    setTotalSpendLineChart(totalSpendLineChartData);

    const categoriesChartTemp: ICategoriesChart[] = [];
    if (data) {
      data.categories.forEach((item) => {
        let barChart: ICategoriesChart = {
          title: '',
          desc: '',
          series: [],
        };
        barChart.title = item.product_name;
        barChart.desc = `$${item.total_spend?.toFixed(2)} Total ${item.total_quantity} ${item.unit}`;
        item?.series.forEach((series) => {
          const date = series.date;
          const quantity = item.quantity_series.find((x) => x.date === date);
          barChart.series.push({
            name: date,
            quantity: quantity?.quantity || 0,
            spend: series.spend || 0,
          });
        });
        categoriesChartTemp.push(barChart);
      });
    }
    setCategoriesChart(categoriesChartTemp);
  }, [data]);

  return { totalSpendLineChart, categoriesChart, loading };
};
