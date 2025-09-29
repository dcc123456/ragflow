import React, { useMemo } from 'react';

interface CustomProgressProps {
  value: number;
  height?: number;
  limit: number;
  basicCapacity?: number;
}

const CustomProgress: React.FC<CustomProgressProps> = ({
  value,
  limit,
  height = 8,
  basicCapacity,
}) => {
  const allPercentage = useMemo(() => {
    return (value / limit) * 100;
  }, [value, limit]);

  const percentage = useMemo(
    () => ((value - (basicCapacity || 0)) / limit) * 100,
    [value, limit, basicCapacity],
  );

  const colorKey = useMemo(() => {
    if (allPercentage <= 30) {
      return 30;
    }
    if (allPercentage > 30 && allPercentage <= 50) {
      return 50;
    }
    if (allPercentage > 50 && allPercentage <= 70) {
      return 70;
    }
    if (allPercentage > 70 && allPercentage <= 99) {
      return 90;
    }
    return 100;
  }, [allPercentage]);
  const colorObj = {
    0: 'linear-gradient(90deg, #1890FF33  0%, #1890FF33  100%)',
    30: 'linear-gradient(90deg, #1890FF 0%, #1890FF 100%)',
    50: 'linear-gradient(90deg, #1890FF 0%, #e67070 100%)',
    70: 'linear-gradient(90deg, #1890FF 0%, #ffc107 100%)',
    90: 'linear-gradient(90deg, #1890FF 0%, #FF4D4F 100%)',
    100: 'linear-gradient(90deg, #FF4D4F 0%, #FF4D4F 100%)',
  };

  //Basic capacity
  const basicCapacityAllPercentage = useMemo(() => {
    if (!basicCapacity || !limit) return 0;
    return (basicCapacity / limit) * 100;
  }, [basicCapacity, limit]);
  const basicCapacityPercentage = useMemo(() => {
    if (!basicCapacity || !limit) return 0;
    return (value / basicCapacity) * 100;
  }, [basicCapacity, limit, value]);

  return (
    <div
      className="flex bg-gray-700 rounded-full relative my-6"
      style={{ height: height + 'px', overflow: 'hidden' }}
    >
      <div
        className="flex bg-[#1890FF33] rounded-full relative"
        style={{
          height: height + 'px',
          width: `${basicCapacityAllPercentage}%`,
          overflow: 'hidden',
        }}
      >
        <div
          className="h-full rounded-full mr-[1px]"
          style={{
            width: `${basicCapacityPercentage}%`,
            background: colorObj[colorKey >= 100 ? 100 : 30],
          }}
        ></div>
      </div>

      <div
        className="h-full rounded-full"
        style={{
          width: `${percentage}%`,
          background:
            colorObj[
              basicCapacity ? (value < basicCapacity ? 0 : colorKey) : colorKey
            ],
        }}
      ></div>
    </div>
  );
};

export default CustomProgress;
