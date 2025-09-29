import { useRef } from 'react';
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis } from 'recharts';
import { useComputedWidth } from '../hook/computed-width';
type IDeepDocBarChartProps = {
  data: { name: string; pages: number; spend: number }[];
  title: string;
  desc: string;
};
const DeepDocBarChart = (porps: IDeepDocBarChartProps) => {
  const { data, title, desc } = porps;
  const deepDocRef = useRef<HTMLDivElement>(null);
  const { width } = useComputedWidth(deepDocRef);
  const CustomTooltip = ({
    active,
    payload,
  }: {
    active: boolean;
    payload: any;
    label: string;
  }) => {
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
          <div className="bg-black border p-2 border-white">
            {Object.entries(payload[0].payload).map(([key, value]) => {
              if (key === 'name') {
                return (
                  <p className="text-foreground font-medium" key={key}>
                    {value as number | string}
                  </p>
                );
              } else {
                return (
                  <p className="text-muted-foreground" key={key}>
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
    <div ref={deepDocRef} className="bg-white/10 p-4 rounded mb-4">
      <h2 className="text-white text-lg font-semibold mb-2">{title}</h2>
      <p className="text-muted-foreground">{desc}</p>
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
          dataKey="pages"
          fill="#4CA4E7"
          background={false}
          radius={[4, 4, 4, 4]}
        />
      </BarChart>
    </div>
  );
};

export default DeepDocBarChart;
