import { cn } from '@/lib/utils';
import { useState } from 'react';
import BillingHistory from './billing-history';
import { Overview } from './overview';
import UsagePage from './usage';

const Billing = () => {
  const [activeKey, setActiveKey] = useState('overview');
  const navList: Record<string, string>[] = [
    {
      key: 'overview',
      values: 'Overview',
    },
    {
      key: 'usage',
      values: 'Usage',
    },
    {
      key: 'billing-history',
      values: 'Billing History',
    },
  ];
  const navClickFunc = (e: Record<string, string>) => {
    console.log(e);
    setActiveKey(e.key);
  };
  return (
    <div className="bg-black text-white p-4 h-full">
      <nav className="flex space-x-4 text-white mb-6">
        {navList.map((item) => (
          <span
            key={item.key}
            className={cn('cursor-pointer', {
              'border-b-2 border-white': item.key === activeKey,
            })}
            onClick={() => navClickFunc(item)}
          >
            {item.values}
          </span>
        ))}
      </nav>
      {activeKey === 'overview' && <Overview />}
      {activeKey === 'usage' && <UsagePage />}
      {activeKey === 'billing-history' && <BillingHistory />}
    </div>
  );
};

export default Billing;
