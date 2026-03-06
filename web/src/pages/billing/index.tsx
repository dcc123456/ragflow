import {
  Segmented,
  SegmentedLabeledOption,
  SegmentedValue,
} from '@/components/ui/segmented';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import UpgradeButton from '../price/price-modal/to-upgrade-button';
import BillingHistory from './billing-history';
import { Overview } from './overview';
import PointsPage from './points';
import UsagePage from './usage';

const Billing = () => {
  const [activeKey, setActiveKey] = useState<SegmentedValue>('overview');
  const { t } = useTranslation();
  const navList: SegmentedLabeledOption[] = [
    {
      value: 'overview',
      label: t('billing.overview'),
    },
    {
      value: 'usage',
      label: t('billing.usage'),
    },
    {
      value: 'billing-history',
      label: t('billing.billingHistory'),
    },
    {
      value: 'points',
      label: 'Points',
    },
  ];

  const navClickFunc = (e: SegmentedValue) => {
    setActiveKey(e);
  };
  return (
    <div className="bg-bg-base text-text-primary p-4 h-[calc(100vh-120px)] overflow-auto w-full">
      <nav className="flex justify-between items-center mb-6">
        <Segmented
          options={navList}
          value={activeKey}
          onChange={navClickFunc}
        ></Segmented>
        <div>
          <span className="text-text-secondary mr-4">
            {t('billing.needMore')}
          </span>
          <UpgradeButton />
        </div>
      </nav>
      {activeKey === 'overview' && <Overview />}
      {activeKey === 'usage' && <UsagePage />}
      {activeKey === 'billing-history' && <BillingHistory />}
      {activeKey === 'points' && <PointsPage />}
    </div>
  );
};

export default Billing;
