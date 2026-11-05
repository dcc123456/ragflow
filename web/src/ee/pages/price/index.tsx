// pages/PricingPage.tsx
import Spotlight from '@/components/spotlight';
import React from 'react';
import AddOnCalculator from '@/ee/pages/price/components/add-on-calculator';
import ComparisonTable from '@/ee/pages/price/components/ComparisonTable';
import FAQs from '@/ee/pages/price/components/faq-s';
import FxGradientText from '@/ee/pages/price/components/FxGradientText';
import { features, PriceName } from '@/ee/pages/price/constant';
import PricingPlan from '@/ee/pages/price/pricing-plan';

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

  const planNames = Object.values(PriceName);

  return (
    <div className="min-h-screen text-text-primary p-10 flex justify-center items-start overflow-auto h-full">
      <div className="w-[1500px]">
        <div className="text-[64px] leading-[80px] font-medium mb-[130px] text-center text-text-primary">
          <FxGradientText preset="primary" direction="right">
            Scale Your Business with RAGFlow
          </FxGradientText>
        </div>

        <PricingPlan isUpgrade={false} />

        {false && <AddOnCalculator />}
        {false && <FAQs faqs={faqs} />}
        {false && (
          <div className="my-10">
            <ComparisonTable features={features} planNames={planNames} />
          </div>
        )}
        <Spotlight opcity={0.4} coverage={60} color="#00BEB4" />
      </div>
    </div>
  );
};

export default PricingPage;
