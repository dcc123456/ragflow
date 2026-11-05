// src/components/ComparisonTable.tsx
import { Check } from 'lucide-react';
import React from 'react';

export interface FeatureItem {
  name: string;
  free?: boolean | string;
  starter?: boolean | string;
  pro?: boolean | string;
  enterprise?: boolean | string;
}

interface ComparisonTableProps {
  features: FeatureItem[];
  planNames: string[]; // e.g., ['Free', 'Starter', 'Pro', 'Enterprise']
}

const ComparisonTable: React.FC<ComparisonTableProps> = ({
  features,
  planNames,
}) => {
  return (
    <div className="overflow-x-auto bg-bg-title rounded-xl border border-border-default shadow-lg">
      <table className="w-full text-left text-sm text-text-primary">
        <thead>
          <tr className="border-b border-border-default">
            <th className="px-6 py-4 font-semibold text-text-primary border-r border-border-default first:border-l-0 last:border-r-0">
              Features
            </th>
            {planNames.map((plan) => (
              <th
                key={plan}
                className="px-6 py-4 font-semibold text-center text-text-primary border-r border-border-default last:border-r-0"
              >
                {plan}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {features.map((feature, idx) => (
            <tr
              key={idx}
              className={`border-border-default hover:bg-bg-list transition-colors bg-bg-base`}
            >
              <td className="px-6 py-4 font-medium text-text-primary border-r border-border-default last:border-r-0">
                {feature.name}
              </td>
              {planNames.map((plan) => {
                const value = feature[plan.toLowerCase() as keyof FeatureItem];
                let content;

                if (value === true) {
                  content = (
                    <div className="flex items-center justify-center text-text-secondary">
                      <Check size={12} />
                    </div>
                  );
                } else {
                  content = (
                    <span className="text-center text-text-secondary">
                      {value || '-'}
                    </span>
                  );
                }

                return (
                  <td
                    key={plan}
                    className="px-6 py-4 text-center border-r border-border-default last:border-r-0"
                  >
                    {content}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

export default ComparisonTable;
