// pages/PricingPage.tsx
import { Modal } from '@/components/ui/modal/modal';
import { convertBytesToGb } from '@/lib/utils';
import { t } from 'i18next';
import { Building2, Check, Gem, LucideProps, Rocket, X } from 'lucide-react';
import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { JSX } from 'react/jsx-runtime';
import { useSearchParams } from 'umi';
import PricingCard from '../components/pricing-card';
import { PriceName } from '../contant';
import { useFetchCurrentPlan, useFetchPlanList } from '../hook/use-price-hooks';
import { IPricePlanWithButton } from '../interface';
import { showModal } from '../price-modal/show-modal';

const pricingPlans = {
  [PriceName.Trial]: {
    id: 'price_1RWUhlPtsKvwvC5fJHfaYeRs',
    title: t('price.free'),
    description: t('price.freeDesc'),
    price: '',
    feature: {
      apps: '',
      teamMembers: '',
      datasetStorage: '',
      apiRequests: '',
    },
    buttonLabel: t('price.reduce'),
    isUse: true,
    icon: () => <></>,
  },
  [PriceName.Starter]: {
    id: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
    title: t('price.starter'),
    description: t('price.starterDesc'),
    price: '',
    feature: {
      apps: '',
      teamMembers: '',
      datasetStorage: '',
      apiRequests: '',
    },
    buttonLabel: t('price.upgrade'),
    isUse: false,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Rocket {...props} />;
    },
  },
  [PriceName.Pro]: {
    id: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
    title: t('price.pro'),
    description: t('price.proDesc'),
    price: '',
    feature: {
      apps: '',
      teamMembers: '',
      datasetStorage: '',
      apiRequests: '',
    },
    buttonLabel: t('price.upgrade'),
    isUse: false,
    isPopular: true,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Gem {...props} />;
    },
  },
  [PriceName.Enterprise]: {
    id: 'Enterprise',
    title: t('price.enterprise'),
    description: t('price.enterpriseDesc'),
    price: '?',
    feature: {
      apps: '?',
      teamMembers: '?',
      datasetStorage: '?',
      apiRequests: '?',
    },
    buttonLabel: t('price.contactUs'),
    isUse: false,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Building2 {...props} />;
    },
  },
};

const PricingPlan = ({ isUpgrade = false }: { isUpgrade: boolean }) => {
  const { data: currentPlan } = useFetchCurrentPlan();
  const { data: planList } = useFetchPlanList();
  const [pricePlanList, setPricePlanList] = useState<IPricePlanWithButton[]>();
  const [searchParams, setSearchParams] = useSearchParams();
  const status = searchParams.get('price-pay-status');
  const { t } = useTranslation();
  const openSuccessModal = useCallback(
    (status: string) => {
      const title = () => {
        switch (status) {
          case 'success':
            return (
              <div className="flex gap-2 items-center">
                <div className="p-1 w-5 h-5 flex items-center justify-center rounded-full bg-green-500">
                  <Check size={14} fontWeight={'bold'} />
                </div>
                Success
              </div>
            );
          case 'cancel':
            return (
              <div className="flex gap-2 items-center">
                <div className="p-1 w-5 h-5 flex items-center justify-center rounded-full bg-red-500">
                  <X size={14} fontWeight={'bold'} />
                </div>
                Error
              </div>
            );
          default:
            return 'Success';
        }
      };
      const content = () => {
        switch (status) {
          case 'success':
            return (
              <div>
                <div className="flex items-center gap-2">
                  payment successful
                </div>
              </div>
            );
          case 'error':
            return (
              <div>
                <div className="flex items-center gap-2">payment Error</div>
              </div>
            );
          default:
            return 'Success';
        }
      };
      if (status) {
        // searchParams.delete('status');
        setSearchParams(searchParams);
        const successModal = showModal({
          children: (
            <Modal
              open={true}
              title={title()}
              onOpenChange={(open) => {
                if (!open) {
                  const newSearchParams = new URLSearchParams(searchParams);
                  newSearchParams.delete('price-pay-status');
                  setSearchParams(newSearchParams);
                  successModal.destroy();
                }
              }}
              className="!w-[400px]"
              footer={
                <div className="flex justify-end gap-2 ">
                  <button
                    type="button"
                    onClick={() => {
                      const newSearchParams = new URLSearchParams(searchParams);
                      newSearchParams.delete('price-pay-status');
                      setSearchParams(newSearchParams);
                      successModal.destroy();
                    }}
                    className="px-2 py-1 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
                  >
                    {t('modal.okText')}
                  </button>
                </div>
              }
            >
              <div className="h-32">{content()}</div>
            </Modal>
          ),
        });
      }
    },
    [searchParams, setSearchParams, t],
  );

  useEffect(() => {
    if (!currentPlan || !planList || planList.length <= 0) return;
    let inUseIndex = 4;

    let plans = planList?.map((plan, index) => {
      let tempPlan = {
        ...pricingPlans[plan.name as keyof typeof pricingPlans],
        name: plan.name,
        feature: {
          apps: plan.feature.quota_apps,
          teamMembers: plan.feature.quota_members,
          datasetStorage: convertBytesToGb(plan.feature.quota_kb_storage),
          apiRequests: plan.feature.quota_api_limits,
        },
        id: plan.price_ids,
        price: plan.price,
        isUse: false,
      };

      if (plan.name && currentPlan.plan_name === plan.name) {
        inUseIndex = index;
        return {
          ...tempPlan,
          isUse: true,
          buttonLabel: t('price.inUse'),
        };
      } else {
        const buttonLabel =
          index < inUseIndex
            ? t('price.reduce')
            : index < planList.length - 1
              ? t('price.upgrade')
              : t('price.contactUs');
        return {
          ...tempPlan,
          buttonLabel,
        };
      }
    });
    // let planArr = planList;
    if (isUpgrade) {
      plans = plans.filter((plan) => plan.name !== PriceName.Trial);
    }
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
      {pricePlanList?.map((plan, index) => (
        <PricingCard key={index} {...plan} />
      ))}
      {/* <PriceModalComponent isOpen={true} onClose={closeModal} /> */}
    </>
  );
};

export default PricingPlan;
