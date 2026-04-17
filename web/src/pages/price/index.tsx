// pages/PricingPage.tsx
import Spotlight from '@/components/spotlight';
import React from 'react';
import AddOnCalculator from './components/add-on-calculator';
import FAQs from './components/faq-s';
import PricingPlan from './pricing-plan';

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

  return (
    <div className="min-h-screen text-text-primary p-10 flex justify-center items-start overflow-auto h-full">
      <div className="w-[1500px]">
        <div className="text-[64px] leading-[80px] font-medium mb-10 text-center text-text-primary">
          Scale Your Business with RAG engine
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-10">
          <PricingPlan isUpgrade={false} />
        </div>
        <AddOnCalculator />
        <FAQs faqs={faqs} />
        <Spotlight />
      </div>
    </div>
  );
};

export default PricingPage;
