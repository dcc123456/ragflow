import { Modal } from '@/components/ui/modal/modal';
import Space from '@/components/ui/space';
import { useState } from 'react';
import { useCharge } from '../hook/use-price-hooks';
import { IPricePlan } from '../interface';
export const ConfirmModal: React.FC<{
  plan: IPricePlan;
  isOpen: boolean;
  onClose: () => void;
}> = ({ plan, isOpen = true, onClose = () => {} }) => {
  const [planId, setPlanId] = useState('');
  const priceDifference = '56.8';
  const date = '2023-09-05';
  const price = '9.99';
  const { data, loading } = useCharge({
    subscription_price_id: planId || '',
    quantity: '1',
  });
  const onOk = () => {
    if (plan?.id) {
      setPlanId(plan.id);
    }
    onClose();
  };
  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      onOk={onOk}
      title="Change your plan"
      footer={null}
      className=" !w-[600px]"
      confirmLoading={loading}
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
// let currentPriceConfirmModal: { destroy: () => void } | null = null;

// const showPriceComfirmModal = ({
//   container,
//   ...plan
// }: IPricePlanWithButton & { container?: HTMLElement }) => {
//   const rootElement = document.createElement('div');
//   if (container) {
//     container.appendChild(rootElement);
//   } else {
//     document.body.appendChild(rootElement);
//   }

//   const reactRoot = createRoot(rootElement);
//   const closeModal = () => {
//     reactRoot.unmount();
//     if (container) {
//       container.removeChild(rootElement);
//     } else {
//       document.body.removeChild(rootElement);
//     }
//     currentPriceConfirmModal = null;
//   };

//   reactRoot.render(
//     <ConfirmModal plan={plan} isOpen={true} onClose={closeModal} />,
//   );

//   currentPriceConfirmModal = { destroy: closeModal };

//   return currentPriceConfirmModal;
// };
// export { showPriceComfirmModal };
