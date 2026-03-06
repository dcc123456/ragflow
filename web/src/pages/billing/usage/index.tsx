import { DateRange } from '@/components/originui/calendar';
import { subDays } from 'date-fns';
import { useState } from 'react';
import CustomBarChart from '../component/bar-chart';
import TimeRangePicker from '../component/time-range-picker';
import TotalSpendLineChart from '../component/total-spend-line-chart';
import { useAllSpends } from '../hook/usage';

const today = new Date();
// const yesterday = {
//   from: subDays(today, 1),
//   to: subDays(today, 1),
// };
const last7Days = {
  from: subDays(today, 6),
  to: today,
};
// const last30Days = {
//   from: subDays(today, 29),
//   to: today,
// };
// const monthToDate = {
//   from: startOfMonth(today),
//   to: today,
// };
// const lastMonth = {
//   from: startOfMonth(subMonths(today, 1)),
//   to: endOfMonth(subMonths(today, 1)),
// };
// const yearToDate = {
//   from: startOfYear(today),
//   to: today,
// };
// const lastYear = {
//   from: startOfYear(subYears(today, 1)),
//   to: endOfYear(subYears(today, 1)),
// };
const UsagePage = () => {
  const [currentDate, setCurrentDate] = useState<DateRange>(last7Days);

  const { totalSpendLineChart, categoriesChart } = useAllSpends(currentDate);
  // const { deepDocBarChart } = useDeepDocSpends(currentDate);
  // const { embeddingBarChart } = useEmbeddingSpends(currentDate);

  const handleDateRangeChange = ({
    from: startDate,
    to: endDate,
  }: DateRange) => {
    console.log('selectDate', startDate, endDate);
    setCurrentDate({ from: startDate, to: endDate });
  };

  return (
    <div className=" text-text-primary p-4">
      <h1 className="text-2xl font-bold">Usage</h1>
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
        desc={`$${totalSpendLineChart.value?.toFixed(2)}`}
      />
      {categoriesChart && categoriesChart?.length > 0 && (
        <>
          <h1 className="text-2xl font-bold mb-4 mt-9">Spend Categories</h1>
          <div className="grid grid-cols-2 gap-4">
            {categoriesChart.map((item, index) => (
              <CustomBarChart
                key={index}
                data={item.series}
                title={item.title}
                desc={item.desc}
              />
            ))}
            {/* <DeepDocBarChart
          data={deepDocBarChart.data}
          title={'DeepDoc'}
          desc={`$${deepDocBarChart.value?.toFixed(2)} Total ${deepDocBarChart.pages} Pages`}
        />
        <EmbeddingTokenBarChart
          data={embeddingBarChart.data}
          title={'Embedding'}
          desc={`$${embeddingBarChart.value?.toFixed(2)} Total ${embeddingBarChart.tokens} Tokens`}
        /> */}
          </div>
        </>
      )}
    </div>
  );
};

export default UsagePage;
