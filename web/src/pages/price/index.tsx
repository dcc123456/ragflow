// pages/PricingPage.tsx
import { Modal } from '@/components/ui/modal';
import { Building2, Check, Gem, LucideProps, Rocket, X } from 'lucide-react';
import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { JSX } from 'react/jsx-runtime';
import { useSearchParams } from 'umi';
import AddOnCalculator from './components/add-on-calculator';
import FAQs from './components/faq-s';
import PricingCard from './components/pricing-card';
import { useFetchCurrentPlan } from './hook/use-price-hooks';
import { IPricePlanWithButton } from './interface';
import { showModal } from './price-modal/show-modal';

const pricingPlans = [
  {
    id: 'price_1RWUhlPtsKvwvC5fJHfaYeRs',
    title: 'Free',
    description:
      'Start for free and explore essential features to get your project off the ground.',
    price: '0',
    feature: {
      apps: '20',
      teamMembers: '50',
      datasetStorage: '5',
      apiRequests: '6000',
    },
    buttonLabel: 'Reduce Now',
    isUse: true,
    icon: () => <></>,
  },
  {
    id: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
    title: 'Starter',
    description:
      'Ideal for individuals and small teams starting their journey with essential features.',
    price: '9.9',
    feature: {
      apps: '40',
      teamMembers: '100',
      datasetStorage: '10',
      apiRequests: '12000',
    },
    buttonLabel: 'Upgrade Now',
    isUse: false,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Rocket {...props} />;
    },
  },
  {
    id: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
    title: 'Pro',
    description:
      'Perfect for growing businesses requiring more advanced tools and higher limits.',
    price: '99',
    feature: {
      apps: '80',
      teamMembers: '200',
      datasetStorage: '20',
      apiRequests: '24000',
    },
    buttonLabel: 'Upgrade Now',
    isUse: false,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Gem {...props} />;
    },
  },
  {
    id: 'Enterprise',
    title: 'Enterprise',
    description:
      'Tailored for large organizations needing custom solutions, priority support, and full scalability',
    price: '?',
    feature: {
      apps: '?',
      teamMembers: '?',
      datasetStorage: '?',
      apiRequests: '?',
    },
    buttonLabel: 'Contact Us',
    isUse: false,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Building2 {...props} />;
    },
  },
];
const planKeyMap = {
  tral: 'Free',
  level1: 'Starter',
  level2: 'Pro',
  level3: 'Enterprise',
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
  const [pricePlanList, setPricePlanList] = useState<IPricePlanWithButton[]>(
    pricingPlans as IPricePlanWithButton[],
  );
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
              onOpenChange={(open) => !open && successModal.destroy()}
              className="!w-[400px]"
              footer={
                <div className="flex justify-end gap-2 ">
                  <button
                    type="button"
                    onClick={() => successModal.destroy()}
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
    if (!currentPlan) return;
    const plans = pricingPlans.map((plan) => {
      if (
        plan.title ===
        planKeyMap[currentPlan.plan_name as keyof typeof planKeyMap]
      ) {
        return {
          ...plan,
          isUse: true,
          buttonLabel: 'In Use',
        } as unknown as IPricePlanWithButton;
      } else {
        return { ...plan, isUse: false } as unknown as IPricePlanWithButton;
      }
    });
    setPricePlanList(plans);
  }, [currentPlan]);
  useEffect(() => {
    if (status) {
      openSuccessModal(status);
    }
  }, [status, openSuccessModal]);

  return (
    <div className="min-h-screen bg-[#101015] text-white p-10 flex justify-center items-start overflow-auto h-full">
      <div className="w-[1500px]">
        <h1 className="text-[68px] leading-[80px] font-bold mb-10 text-center bg-gradient-to-r from-indigo-500 from-30% via-sky-500 via-60% to-emerald-500 bg-clip-text text-transparent">
          Scale Your Business with RAG engine
        </h1>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-10">
          {pricePlanList.map((plan, index) => (
            <PricingCard key={index} {...plan} />
          ))}
        </div>
        <AddOnCalculator />
        <FAQs faqs={faqs} />
      </div>
    </div>
  );
};

export default PricingPage;
