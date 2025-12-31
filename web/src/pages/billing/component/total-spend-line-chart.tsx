import { ChartSpline } from 'lucide-react';
import { useRef } from 'react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { useComputedWidth } from '../hook/computed-width';
type ITotalSpendLineChartProps = {
  data: { name: string; spend: number }[];
  title: string;
  desc: string;
};

const TotalSpendLineChart = (props: ITotalSpendLineChartProps) => {
  const { data, title, desc } = props;

  const totalSpendRef = useRef<HTMLDivElement>(null);
  const { width } = useComputedWidth(totalSpendRef);

  return (
    <div
      ref={totalSpendRef}
      className="total-spend bg-bg-card pt-4 pr-4 pb-4 pl-1 rounded mb-4 w-full"
    >
      <div className="flex justify-start items-start pl-3">
        <div className="p-1 border rounded">
          <ChartSpline size={16} />
        </div>
        <div className="ml-2">
          <h2 className="text-text-primary text-lg font-semibold mb-1">
            {title}
          </h2>
          <p className="text-text-secondary">{desc}</p>
        </div>
      </div>

      <AreaChart width={width || 600} height={300} data={data}>
        <defs>
          <linearGradient id="colorUv" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor="#00BEB4" stopOpacity={0.8} />
            <stop offset="95%" stopColor="#00BEB4" stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid
          strokeDasharray="3 0"
          vertical={false}
          stroke="#2d374835"
        />
        <XAxis dataKey="name" />
        <YAxis axisLine={false} tickLine={false} width={32} />
        <Tooltip contentStyle={{ backgroundColor: 'black' }} />
        <Area
          type="monotone"
          dataKey="spend"
          stroke="#00BEB4"
          fill="url(#colorUv)"
          fillOpacity="1"
        />
      </AreaChart>
    </div>
  );
};

export default TotalSpendLineChart;
