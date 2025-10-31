import {
  Segmented,
  SegmentedLabeledOption,
  SegmentedValue,
} from '@/components/ui/segmented';
import { useState } from 'react';
import UpgradeButton from '../price/price-modal/to-upgrade-button';
import BillingHistory from './billing-history';
import { Overview } from './overview';
import UsagePage from './usage';

const Billing = () => {
  const [activeKey, setActiveKey] = useState<SegmentedValue>('overview');
  const navList: SegmentedLabeledOption[] = [
    {
      value: 'overview',
      label: 'Overview',
    },
    {
      value: 'usage',
      label: 'Usage',
    },
    {
      value: 'billing-history',
      label: 'Billing History',
    },
  ];
  const navClickFunc = (e: SegmentedValue) => {
    console.log(e);
    setActiveKey(e);
  };
  return (
    <div className="bg-bg-base text-text-primary p-4 h-[calc(100vh-80px)] overflow-auto">
      <nav className="flex justify-between items-center mb-6">
        <Segmented
          options={navList}
          value={activeKey}
          onChange={navClickFunc}
        ></Segmented>
        <div>
          <span className="text-text-secondary mr-4">Need more?</span>
          <UpgradeButton />
        </div>
      </nav>
      {activeKey === 'overview' && <Overview />}
      {activeKey === 'usage' && <UsagePage />}
      {activeKey === 'billing-history' && <BillingHistory />}
    </div>
  );
};

export default Billing;
