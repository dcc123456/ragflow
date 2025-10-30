import { Modal } from '@/components/ui/modal/modal';
import PricingPlan from '../pricing-plan';
// const pricingPlans: IPricePlanWithButton[] = [
//   {
//     id: 'price_1RWUhlPtsKvwvC5fJHfaYeRs',
//     title: 'Starter',
//     description:
//       'Ideal for individuals and small teams starting their journey with essential features.',
//     price: '9.9',
//     feature: {
//       apps: '40',
//       teamMembers: '100',
//       datasetStorage: '10',
//       apiRequests: '12000',
//     },
//     buttonLabel: 'Upgrade Now',
//     isUse: false,
//     icon: (
//       props?: JSX.IntrinsicAttributes &
//         Omit<LucideProps, 'ref'> &
//         React.RefAttributes<SVGSVGElement>,
//     ) => {
//       return <Rocket {...props} />;
//     },
//   },
//   {
//     id: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
//     title: 'Pro',
//     description:
//       'Perfect for growing businesses requiring more advanced tools and higher limits.',
//     price: '99',
//     feature: {
//       apps: '80',
//       teamMembers: '200',
//       datasetStorage: '20',
//       apiRequests: '24000',
//     },
//     buttonLabel: 'Upgrade Now',
//     isUse: false,
//     icon: (
//       props?: JSX.IntrinsicAttributes &
//         Omit<LucideProps, 'ref'> &
//         React.RefAttributes<SVGSVGElement>,
//     ) => {
//       return <Gem {...props} />;
//     },
//   },
//   {
//     id: 'Enterprise',
//     title: 'Enterprise',
//     description:
//       'Tailored for large organizations needing custom solutions, priority support, and full scalability',
//     price: '?',
//     feature: {
//       apps: '?',
//       teamMembers: '?',
//       datasetStorage: '?',
//       apiRequests: '?',
//     },
//     buttonLabel: 'Contact Us',
//     isUse: false,
//     icon: (
//       props?: JSX.IntrinsicAttributes &
//         Omit<LucideProps, 'ref'> &
//         React.RefAttributes<SVGSVGElement>,
//     ) => {
//       return <Building2 {...props} />;
//     },
//   },
// ];
interface PriceModalProps {
  isOpen: boolean;
  onClose: () => void;
}
export const PriceModalComponent: React.FC<PriceModalProps> = ({
  isOpen,
  onClose,
}) => {
  if (!isOpen) return null;
  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      closable={false}
      showfooter={false}
      footer={<></>}
      className="!w-[1100px] max-w-[1100px] !bg-bg-base"
    >
      <div className="flex flex-col items-center justify-center p-6 !bg-bg-base rounded-lg text-text-primary">
        <h2 className="text-xl font-bold mb-4 self-start">Manage Plan</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-10">
          {/* {pricingPlans.map((plan, index) => (
            <PricingCard key={index} {...plan} />
          ))} */}
          <PricingPlan isUpgrade={true} />
        </div>
      </div>
    </Modal>
  );
};
