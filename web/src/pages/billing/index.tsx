import {
  Segmented,
  SegmentedLabeledOption,
  SegmentedValue,
} from '@/components/ui/segmented';
import { createContext, useContext, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import UpgradeButton from '../price/price-modal/to-upgrade-button';
import BillingHistory from './billing-history';
import { useFetchUsageBasedPlans } from './hook/use-usage-base-plans';
import { BillingContextType } from './interface';
import { Overview } from './overview';
import UsagePage from './usage';

const BillingContext = createContext<
  { usageBasedPlans: BillingContextType[] } | undefined
>(undefined);
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
  ];

  const { data: usageBasedPlans } = useFetchUsageBasedPlans();

  useEffect(() => {
    if (usageBasedPlans) {
      console.log('usageBasedPlans', usageBasedPlans);
    }
  }, [usageBasedPlans]);

  const navClickFunc = (e: SegmentedValue) => {
    console.log(e);
    setActiveKey(e);
  };
  return (
    <BillingContext.Provider
      value={{
        usageBasedPlans: usageBasedPlans || [],
      }}
    >
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
      </div>
    </BillingContext.Provider>
  );
};

export default Billing;

export const useBillingContext = () => {
  const context = useContext(BillingContext);
  if (context === undefined) {
    throw new Error(
      'useBillingContext must be used within a BillingContextProvider',
    );
  }
  return context;
};
