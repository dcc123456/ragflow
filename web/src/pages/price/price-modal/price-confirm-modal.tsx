import { Modal } from '@/components/ui/modal/modal';
import Space from '@/components/ui/space';
import { memo, useState } from 'react';
import { getNextMonth, useCharge } from '../hook/use-price-hooks';
import { IConfirmPlan, IPricePlanWithButton } from '../interface';
export const ConfirmModal: React.FC<{
  plan: IConfirmPlan;
  isOpen: boolean;
  onClose: () => void;
}> = memo(({ plan, isOpen = true, onClose = () => {} }) => {
  // const [planId, setPlanId] = useState('');
  const priceDifference = plan.priceDifference;
  const date = getNextMonth.getDayFormatted(getNextMonth.get31Day());
  const price = plan.price;
  const { charge } = useCharge();
  const [loading, setLoading] = useState(false);
  const onOk = async () => {
    if (!plan?.id) {
      return;
    }
    setLoading(true);
    try {
      await charge(plan as unknown as IPricePlanWithButton);
    } catch (e) {
      // charge() shows its own error message; we just ensure spinner stops
      console.error('Failed to inspect blob header', e);
      return;
    } finally {
      setLoading(false);
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
});

ConfirmModal.displayName = 'ConfirmModal';
