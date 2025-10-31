import { Modal } from '@/components/ui/modal/modal';
import Space from '@/components/ui/space';
import { createRoot } from 'react-dom/client';
import { IPricePlanWithButton } from '../interface';
const ConfirmModal: React.FC<{
  plan: IPricePlanWithButton;
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
        <div className="text-sm text-text-primary">
          You are changing to the&nbsp;&nbsp;
          <span className="text-base">{plan.title} Plan. </span>
          Based on your current plan, you need to pay the following prorated
          charge.
        </div>
        <Space align="center">
          <div className="text-text-primary text-sm">Prorated Charge:</div>
          <div className="text-text-primary font-bold text-base">
            ${priceDifference}
          </div>
        </Space>
        <div className="bg-accent-primary-5 rounded-md p-2 flex flex-col gap-2 mb-6 text-text-secondary text-sm">
          <div className="text-xs">Next Billing</div>
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

const showPriceComfirmModal = (plan: IPricePlanWithButton) => {
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
