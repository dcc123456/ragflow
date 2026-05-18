import {
  Segmented,
  SegmentedLabeledOption,
  SegmentedValue,
} from '@/components/ui/segmented';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PriceName } from '../price/constant';
import {
  useFetchCurrentPlan,
  useHandleTrialUpgradeSetupRetry,
} from '../price/hook/use-price-hooks';
import UpgradeButton from '../price/price-modal/to-upgrade-button';
import BillingHistory from './billing-history';
import { Overview } from './overview';
import PointsPage from './points';

const Billing = () => {
  const [activeKey, setActiveKey] = useState<SegmentedValue>('overview');
  const { t } = useTranslation();
  const { data: currentPlan } = useFetchCurrentPlan();
  const status = new URLSearchParams(window.location.search).get(
    'price-pay-status',
  );
  useHandleTrialUpgradeSetupRetry(status);
  const navList: SegmentedLabeledOption[] = [
    {
      value: 'overview',
      label: t('billing.overview'),
    },
    {
      value: 'billing-history',
      label: t('billing.billingHistory'),
    },
    {
      value: 'points',
      label: 'Points',
    },
  ].filter((x) => x.value !== 'usage');

  const navClickFunc = (e: SegmentedValue) => {
    setActiveKey(e);
  };
  return (
    <div className="bg-bg-base text-text-primary p-4 h-[calc(100vh-120px)] w-full flex flex-col">
      <nav className="flex justify-between items-center mb-6">
        <Segmented
          options={navList}
          value={activeKey}
          onChange={navClickFunc}
        ></Segmented>
        <div>
          <span className="text-text-secondary mr-4 text-sm">
            {t('billing.needMore')}
          </span>
          <UpgradeButton
            className="text-sm"
            text={
              currentPlan?.plan_name !== PriceName.Trial
                ? t('billing.changePlan')
                : t('billing.upgradePlan')
            }
          />
        </div>
      </nav>
      <section className="flex-1 overflow-auto">
        {activeKey === 'overview' && <Overview />}
        {activeKey === 'billing-history' && <BillingHistory />}
        {activeKey === 'points' && <PointsPage />}
      </section>
    </div>
  );
};

export default Billing;
