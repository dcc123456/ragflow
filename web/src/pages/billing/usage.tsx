import { DateRange } from '@/components/originui/calendar';
import { useState } from 'react';
import DeepDocBarChart from './component/deep-doc-bar-chart';
import EmbeddingTokenBarChart from './component/embedding-token-bar-chart';
import TimeRangePicker from './component/time-range-picker';
import TotalSpendLineChart from './component/total-spend-line-chart';

const UsagePage = () => {
  const [currentDate, setCurrentDate] = useState<DateRange>({
    from: new Date(),
    to: new Date(),
  });
  const handleDateRangeChange = ({
    from: startDate,
    to: endDate,
  }: DateRange) => {
    console.log(startDate, endDate);
  };
  const totalSpendLineChart = {
    data: [
      { name: 'Apr 10', spend: 8 },
      { name: 'Apr 15', spend: 12 },
      { name: 'Apr 20', spend: 7 },
      { name: 'Apr 25', spend: 15 },
      { name: 'Apr 30', spend: 9 },
      { name: 'May 5', spend: 14 },
      { name: 'May 10', spend: 10 },
      { name: 'May 15', spend: 16 },
      { name: 'May 20', spend: 11 },
      { name: 'May 25', spend: 13 },
      { name: 'May 30', spend: 8 },
    ],
    title: 'Total Spend',
    value: 7890,
  };

  const deepDocBarChart = {
    data: [
      { name: 'Apr 5', pages: 10, spend: 12.85 },
      { name: 'Apr 10', pages: 15, spend: 12.85 },
      { name: 'Apr 15', pages: 8, spend: 12.85 },
      { name: 'Apr 20', pages: 12, spend: 12.85 },
      { name: 'Apr 25', pages: 14, spend: 12.85 },
      { name: 'Apr 26', pages: 14, spend: 12.85 },
      { name: 'Apr 27', pages: 14, spend: 12.85 },
    ],
    value: '12.85',
    pages: 999,
  };
  const embeddingTokenBarChart = {
    data: [
      { name: 'Apr 5', tokens: 10, spend: 12.85 },
      { name: 'Apr 10', tokens: 15, spend: 12.85 },
      { name: 'Apr 15', tokens: 8, spend: 12.85 },
      { name: 'Apr 20', tokens: 12, spend: 12.85 },
      { name: 'Apr 25', tokens: 14, spend: 12.85 },
    ],
    value: '12.85',
    tokens: 1999,
  };
  return (
    <div className="bg-black text-white p-4">
      <h1 className="text-2xl font-bold mb-4">Usage</h1>
      <div className="w-full flex justify-between items-end">
        <p className="mb-4">Showing total visitors for the last 7 days</p>
        <div className="p-4">
          <TimeRangePicker
            onSelect={handleDateRangeChange}
            selectDateRange={{ from: currentDate.from, to: currentDate.to }}
          />
        </div>
      </div>
      <TotalSpendLineChart
        data={totalSpendLineChart.data}
        title="Total Spend"
        desc={`$${totalSpendLineChart.value}`}
      />
      <h1 className="text-2xl font-bold mb-4 mt-9">Spend Categories</h1>
      <div className="grid grid-cols-2 gap-4">
        <DeepDocBarChart
          data={deepDocBarChart.data}
          title={'DeepDoc'}
          desc={`$${deepDocBarChart.value} Total ${deepDocBarChart.pages} Pages`}
        />
        <EmbeddingTokenBarChart
          data={embeddingTokenBarChart.data}
          title={'Embedding'}
          desc={`$${embeddingTokenBarChart.value} Total ${embeddingTokenBarChart.tokens} Tokens`}
        />
      </div>
    </div>
  );
};

export default UsagePage;
