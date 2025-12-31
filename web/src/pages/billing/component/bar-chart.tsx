import { useRef } from 'react';
import {
  Bar,
  BarChart,
  CartesianGrid,
  Tooltip,
  TooltipProps,
  XAxis,
} from 'recharts';
import { useComputedWidth } from '../hook/computed-width';
type IDeepDocBarChartProps = {
  data: { name: string; quantity: number; spend: number }[];
  title: string;
  desc: string;
};
const CustomBarChart = (props: IDeepDocBarChartProps) => {
  const { data, title, desc } = props;
  const deepDocRef = useRef<HTMLDivElement>(null);
  const { width } = useComputedWidth(deepDocRef);
  const CustomTooltip = ({ active, payload }: TooltipProps<number, string>) => {
    const isVisible = active && payload && payload.length;
    if (isVisible) {
      console.log(payload);
    }
    return (
      <div
        className="custom-tooltip"
        style={{ visibility: isVisible ? 'visible' : 'hidden' }}
      >
        {isVisible && (
          <div className="bg-bg-base border p-2 border-border-button">
            {Object.entries(payload[0].payload).map(([key, value]) => {
              if (key === 'name') {
                return (
                  <p className="text-text-primary font-medium" key={key}>
                    {value as number | string}
                  </p>
                );
              } else {
                return (
                  <p className="text-text-secondary" key={key}>
                    {key}: {value as number | string}
                  </p>
                );
              }
            })}
          </div>
        )}
      </div>
    );
  };
  return (
    <div ref={deepDocRef} className="bg-bg-card p-4 rounded mb-4">
      <h2 className="text-text-primary text-lg font-semibold mb-2">{title}</h2>
      <p className="text-text-secondary">{desc}</p>
      <BarChart width={width || 600} height={300} data={data} barSize={20}>
        <CartesianGrid
          strokeDasharray="3 0"
          vertical={false}
          stroke="#2d374835"
        />
        <XAxis dataKey="name" scale="point" padding={{ left: 20, right: 20 }} />
        <Tooltip
          contentStyle={{ backgroundColor: 'black' }}
          content={CustomTooltip}
        />
        <Bar
          dataKey="quantity"
          fill="#00BEB4"
          background={false}
          radius={[4, 4, 0, 0]}
        />
      </BarChart>
    </div>
  );
};

export default CustomBarChart;
