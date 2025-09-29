import { Modal } from '@/components/ui/modal';
import Space from '@/components/ui/space';
import { createRoot } from 'react-dom/client';
import { IPricingCardProps } from './pricing-card';
const ConfirmModal: React.FC<{
  plan: IPricingCardProps;
  isOpen: boolean;
  onClose: () => void;
}> = ({ plan, isOpen = true, onClose = () => {} }) => {
  const priceDifference = '56.8';
  const date = '2023-09-05';
  const price = '9.99';
  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      title="Change your plan"
      footer={null}
      className="!bg-[#0B0B0C] !w-[600px]"
    >
      <Space direction="vertical">
        <div className="text-[16px]">
          You are changing to the&nbsp;&nbsp;
          <span className="font-bold text-xl">{plan.title} Plan. </span>
          Based on your current plan, you need to pay the following prorated
          charge.
        </div>
        <Space align="center">
          <div className="text-card-foreground">Prorated Charge:</div>
          <div className="text-foreground font-bold text-2xl">
            ${priceDifference}
          </div>
        </Space>
        <div className="bg-[#4CA4E7]/5 rounded-md p-2 flex flex-col gap-2 mb-6 text-muted-foreground">
          <div>Next Billing</div>
          <Space align="center">
            <Space>
              <span>Date:</span>
              <span>{date}</span>
            </Space>
            <Space>
              <span>Amount:</span>
              <span>${price}</span>
            </Space>
          </Space>
        </div>
      </Space>
    </Modal>
  );
};
let currentPriceConfirmModal: { destroy: () => void } | null = null;

const showPriceComfirmModal = (plan: IPricingCardProps) => {
  const rootElement = document.createElement('div');
  document.body.appendChild(rootElement);

  const reactRoot = createRoot(rootElement);
  const closeModal = () => {
    reactRoot.unmount();
    document.body.removeChild(rootElement);
    currentPriceConfirmModal = null;
  };

  reactRoot.render(
    <ConfirmModal plan={plan} isOpen={true} onClose={closeModal} />,
  );

  currentPriceConfirmModal = { destroy: closeModal };

  return currentPriceConfirmModal;
};
export { showPriceComfirmModal };
