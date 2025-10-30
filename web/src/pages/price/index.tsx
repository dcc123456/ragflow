// pages/PricingPage.tsx
import Spotlight from '@/components/spotlight';
import { Modal } from '@/components/ui/modal/modal';
import { t } from 'i18next';
import { Building2, Check, Gem, LucideProps, Rocket, X } from 'lucide-react';
import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { JSX } from 'react/jsx-runtime';
import { useSearchParams } from 'umi';
import AddOnCalculator from './components/add-on-calculator';
import FAQs from './components/faq-s';
import PricingCard from './components/pricing-card';
import { useFetchCurrentPlan, useFetchPlanList } from './hook/use-price-hooks';
import { IPricePlanWithButton } from './interface';
import { showModal } from './price-modal/show-modal';

const pricingPlans = {
  Trial: {
    id: 'price_1RWUhlPtsKvwvC5fJHfaYeRs',
    title: t('price.free'),
    description: t('price.freeDesc'),
    price: '0',
    feature: {
      apps: '20',
      teamMembers: '50',
      datasetStorage: '5',
      apiRequests: '6000',
    },
    buttonLabel: t('price.reduce'),
    isUse: true,
    icon: () => <></>,
  },
  Starter: {
    id: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
    title: t('price.starter'),
    description: t('price.starterDesc'),
    price: '9.9',
    feature: {
      apps: '40',
      teamMembers: '100',
      datasetStorage: '10',
      apiRequests: '12000',
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
  Pro: {
    id: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
    title: t('price.pro'),
    description: t('price.proDesc'),
    price: '99',
    feature: {
      apps: '80',
      teamMembers: '200',
      datasetStorage: '20',
      apiRequests: '24000',
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
  Enterprise: {
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

const PricingPage: React.FC = () => {
  const faqs = [
    {
      question: 'Can I get a refund?',
      answer:
        'We currently don’t process automatic refunds, but you can request a manual review by emailing xxxxxx@ragflow.io.',
    },
    {
      question: 'Can I get a refund?',
      answer:
        'We currently don’t process automatic refunds, but you can request a manual review by emailing xxxxxx@ragflow.io.',
    },
    {
      question: 'Can I get a refund?',
      answer:
        'We currently don’t process automatic refunds, but you can request a manual review by emailing xxxxxx@ragflow.io.',
    },
    {
      question: 'Can I get a refund?',
      answer:
        'We currently don’t process automatic refunds, but you can request a manual review by emailing xxxxxx@ragflow.io.',
    },
  ];
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
    const plans = planList?.map((plan, index) => {
      let tempPlan = {
        ...pricingPlans[plan.name as keyof typeof pricingPlans],
        name: plan.name,
        feature: {
          apps: plan.feature.quota_apps,
          teamMembers: plan.feature.quota_members,
          datasetStorage: plan.feature.quota_kb_storage,
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
        } as unknown as IPricePlanWithButton;
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
        } as unknown as IPricePlanWithButton;
      }
    });
    setPricePlanList(plans);
  }, [currentPlan, planList, t]);
  useEffect(() => {
    if (status) {
      openSuccessModal(status);
    }
  }, [status, openSuccessModal]);

  return (
    <div className="min-h-screen text-text-primary p-10 flex justify-center items-start overflow-auto h-full">
      <div className="w-[1500px]">
        {/* <h1 className="text-[68px] leading-[80px] font-bold mb-10 text-center bg-gradient-to-r from-indigo-500 from-30% via-sky-500 via-60% to-emerald-500 bg-clip-text text-transparent"> */}
        <div className="text-[64px] leading-[80px] font-medium mb-10 text-center text-text-primary">
          Scale Your Business with RAG engine
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-10">
          {pricePlanList?.map((plan, index) => (
            <PricingCard key={index} {...plan} />
          ))}
        </div>
        <AddOnCalculator />
        <FAQs faqs={faqs} />
        <Spotlight />
      </div>
    </div>
  );
};

export default PricingPage;
