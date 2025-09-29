import NumberInput from '@/components/originui/number-input';
import { Modal } from '@/components/ui/modal';
import React, { useMemo } from 'react';
import { createRoot } from 'react-dom/client';
interface IOkFuncProps {
  value: number;
}
interface CustomModalProps {
  isOpen: boolean;
  onClose: () => void;
  onOk: (T: IOkFuncProps) => void;
  defaultValue?: number;
}

const CustomModal: React.FC<CustomModalProps> = ({
  isOpen,
  onClose,
  onOk,
  defaultValue = 0,
}) => {
  const [value, setValue] = React.useState(defaultValue);
  const handleChange = (e: number) => {
    setValue(e);
  };
  const price = 0.9;
  const newCost = useMemo(() => {
    return (value * price).toFixed(2);
  }, [value]);
  const handleOk = () => {
    onOk?.({ value });
  };
  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      onOk={handleOk}
      closable={false}
      title={'Manage Add-on storage'}
      className="!w-[500px]"
    >
      <div className="flex flex-col gap-4">
        <div className="flex items-center mb-4 gap-4 justify-between">
          <div className="text-start">Storage</div>
          <div className="flex items-center gap-2">
            <NumberInput value={value} onChange={(e) => handleChange(e)} />
            GB
          </div>
        </div>
        <div className="flex items-center flex-col bg-sky-500/10 p-4 rounded-lg gap-2">
          <div className="flex items-center justify-between w-full">
            <div className="font-thin">Current monthly cost</div>
            <div className="font-normal">${defaultValue * price}</div>
          </div>
          <div className="flex items-center justify-between w-full">
            <div className="font-thin">New monthly cost</div>
            <div className="font-normal">${newCost}</div>
          </div>
        </div>
        <div className="h-12">
          {value < defaultValue && (
            <div>
              <div className="font-thin text-sm">
                Reduced quota takes effect on <b>2025/7/1</b>.
              </div>
              <div className="font-thin text-sm">
                Ensure usage is below <b>60GB</b> to avoid overage.
              </div>
            </div>
          )}
          {value > defaultValue && (
            <div className="font-thin text-sm">
              Pay ${(Number(newCost) - defaultValue * price).toFixed(2)} now
              (prorated) and enjoy extra storage immediately.
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
};

let currentModal: { destroy: () => void } | null = null;
interface IShowUpgradeTipsModalOptions {
  defaultValue: number;
  onOk: (T: IOkFuncProps) => void;
}
const showAddOnManageModal = ({
  defaultValue = 0,
  onOk,
}: IShowUpgradeTipsModalOptions) => {
  const rootElement = document.createElement('div');
  document.body.appendChild(rootElement);

  const reactRoot = createRoot(rootElement);
  const closeModal = () => {
    reactRoot.unmount();
    document.body.removeChild(rootElement);
    currentModal = null;
  };

  reactRoot.render(
    <CustomModal
      isOpen={true}
      onOk={onOk}
      defaultValue={defaultValue}
      onClose={closeModal}
    />,
  );

  currentModal = { destroy: closeModal };

  return currentModal;
};

export { showAddOnManageModal };
