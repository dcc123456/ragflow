// pages/PricingPage.tsx
import { Modal } from '@/components/ui/modal/modal';
import { convertBytesToGb } from '@/lib/utils';
import { t } from 'i18next';
import {
  BanknoteArrowUp,
  Coins,
  DatabaseZap,
  HeartHandshake,
  LayoutGrid,
  Loader2,
  ShieldCheck,
  Users,
  Vault,
  X,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { JSX } from 'react/jsx-runtime';
import PricingCard from '../components/pricing-card';
import { PriceName, PriceNameMapValue } from '../constant';
import { useFetchCurrentPlan, useFetchPlanList } from '../hook/use-price-hooks';
import { IPricePlanWithButton } from '../interface';

const enterprise = {
  id: 'Enterprise',
  title: t('price.enterprise'),
  description: t('price.enterpriseDesc'),
  price: -1,
  buttonLabel: t('price.contactUs'),
  isUse: false,
  features: [
    {
      key: 'apps',
      value: -1,
      name: 'BYOC deployment',
      icon: (
        <BanknoteArrowUp
          size={12}
          className="text-text-primary font-normal mr-2"
        />
      ),
    },
    {
      key: 'teamMembers',
      value: -1,
      name: 'On-premises deployment',
      icon: <Vault size={12} className="text-text-primary font-normal mr-2" />,
    },
    {
      key: 'datasetStorage',
      value: -1,
      name: 'Dedicated support',
      icon: (
        <HeartHandshake
          size={12}
          className="text-text-primary font-normal mr-2"
        />
      ),
    },
    {
      key: 'credits',
      value: -1,
      name: 'Custom SLA',
      icon: (
        <ShieldCheck size={12} className="text-text-primary font-normal mr-2" />
      ),
    },
  ],
};
const commonFeatures = [
  {
    key: 'apps',
    value: '',
    name: 'Apps',
    icon: (
      <LayoutGrid size={12} className="text-text-primary font-normal mr-2" />
    ),
  },
  {
    key: 'teamMembers',
    value: '',
    name: 'team members',
    icon: <Users size={12} className="text-text-primary font-normal mr-2" />,
  },
  {
    key: 'datasetStorage',
    value: '',
    name: 'GB dataset storage',
    icon: (
      <DatabaseZap size={12} className="text-text-primary font-normal mr-2" />
    ),
  },
  {
    key: 'credits',
    value: '',
    name: 'credits / month',
    icon: <Coins size={12} className="text-text-primary font-normal mr-2" />,
  },
];
const pricingPlans = {
  [PriceName.Trial]: {
    id: '',
    title: t('price.free'),
    description: t('price.freeDesc'),
    price: '',
    buttonLabel: t('price.reduce'),
    isUse: true,
    features: commonFeatures,
  },
  [PriceName.Starter]: {
    id: '',
    title: t('price.starter'),
    description: t('price.starterDesc'),
    price: '',
    buttonLabel: t('price.upgrade'),
    isUse: false,
    features: commonFeatures,
  },
  [PriceName.Pro]: {
    id: '',
    title: t('price.pro'),
    description: t('price.proDesc'),
    price: '',
    buttonLabel: t('price.upgrade'),
    isUse: false,
    isPopular: true,
    features: commonFeatures,
  },
  [PriceName.Enterprise]: enterprise,
};

const PricingPlan = ({ isUpgrade = false }: { isUpgrade: boolean }) => {
  const { data: currentPlan } = useFetchCurrentPlan();
  const { data: planList, loading } = useFetchPlanList();
  const [pricePlanList, setPricePlanList] = useState<IPricePlanWithButton[]>();
  const urlParams = useMemo(
    () => new URLSearchParams(window.location.search),
    [],
  );
  // const [searchParams, setSearchParams] = useSearchParams();
  const status = urlParams.get('price-pay-status');
  const { t } = useTranslation();
  const [successModal, setSuccessModal] = useState<{
    title: string | JSX.Element;
    content: string | JSX.Element;
    open: boolean;
  }>({
    title: '',
    content: '',
    open: false,
  });

  const openSuccessModal = useCallback(
    (status: string) => {
      const isPaymentFailed = status === 'cancel' || status === 'error';

      const title = () => {
        if (status === 'success') {
          return (
            <div className="flex gap-2 items-center">
              {t('price.paymentSuccessful')}
            </div>
          );
        }

        if (isPaymentFailed) {
          return (
            <div className="flex gap-2 items-center">
              <div className="p-1 w-5 h-5 flex items-center justify-center rounded-full bg-red-500">
                <X size={14} fontWeight={'bold'} />
              </div>
              {t('price.paymentFailed')}
            </div>
          );
        }

        return '';
      };
      const content = () => {
        if (status === 'success') {
          return (
            <div>
              <div className="flex items-center gap-2">
                {t('price.paymentSuccessfulTip')}
              </div>
            </div>
          );
        }

        if (isPaymentFailed) {
          return (
            <div>
              <div className="flex items-center gap-2">
                {t('price.paymentFailedTip')}
              </div>
            </div>
          );
        }

        return '';
      };
      if (status) {
        setSuccessModal({
          title: title(),
          content: content(),
          open: true,
        });
      }
    },
    [t],
  );

  useEffect(() => {
    if (!currentPlan || !planList || planList.length <= 0) return;
    const currentPlanValue =
      PriceNameMapValue[
        currentPlan.plan_name as keyof typeof PriceNameMapValue
      ] ?? -1;

    let plans = planList?.map((plan) => {
      const featureValue = {
        apps: plan.feature.quota_apps,
        teamMembers: plan.feature.quota_members,
        datasetStorage: convertBytesToGb(plan.feature.quota_kb_storage),
        credits: plan.feature.quota_points,
      };
      const thisPricePlan =
        pricingPlans[plan.name as keyof typeof pricingPlans];
      const planValue =
        PriceNameMapValue[plan.name as keyof typeof PriceNameMapValue] ?? -1;
      const tempPlan = {
        ...thisPricePlan,
        name: plan.name,
        id: plan.price_ids,
        price: plan.price,
        isUse: false,
        disabled: false,
        features: thisPricePlan.features.map((feature) => {
          return {
            ...feature,
            value: featureValue[feature.key as keyof typeof featureValue],
          };
        }),
      };

      if (plan.name && currentPlan.plan_name === plan.name) {
        return {
          ...tempPlan,
          isUse: true,
          buttonLabel: t('price.inUse'),
        };
      } else {
        return {
          ...tempPlan,
          buttonLabel: t('price.upgrade'),
          disabled: planValue < currentPlanValue,
        };
      }
    });

    if (isUpgrade) {
      plans = plans.filter((plan) => plan.name !== PriceName.Trial);
    }
    plans.push({
      ...enterprise,
      name: PriceName.Enterprise,
      isPopular: false,
      disabled: false,
      buttonLabel: t('price.contactUs'),
    });
    setPricePlanList(plans as unknown as IPricePlanWithButton[]);
  }, [currentPlan, planList, t, isUpgrade]);

  useEffect(() => {
    if (status) {
      openSuccessModal(status);
    }
  }, [status, openSuccessModal]);

  //   showPriceModal(ref);
  return (
    <>
      {(loading || !pricePlanList) && (
        <div className="flex justify-center items-center h-[200px] w-full">
          <Loader2 className="animate-spin" />
        </div>
      )}
      <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-10">
        {!loading &&
          pricePlanList?.map((plan, index) => (
            <PricingCard key={index} {...plan} />
          ))}

        {successModal.open && (
          <Modal
            open={true}
            title={successModal.title}
            onOpenChange={(open) => {
              if (!open) {
                const urlObj = new URL(window.location.href);
                urlObj.searchParams.delete('price-pay-status');
                window.history.replaceState({}, '', urlObj.toString());
                // successModal.destroy();
                setSuccessModal({
                  open: false,
                  title: '',
                  content: '',
                });
              }
            }}
            className="!w-[400px]"
            footer={
              <div className="flex justify-end gap-2 ">
                <button
                  type="button"
                  onClick={() => {
                    const urlObj = new URL(window.location.href);
                    urlObj.searchParams.delete('price-pay-status');
                    window.history.replaceState({}, '', urlObj.toString());
                    setSuccessModal({
                      open: false,
                      title: '',
                      content: '',
                    });
                    // successModal.destroy();
                  }}
                  className="px-2 py-1 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
                >
                  {t('modal.okText')}
                </button>
              </div>
            }
          >
            <div className="h-32">{successModal.content}</div>
          </Modal>
        )}
      </div>
    </>
  );
};

export default PricingPlan;
