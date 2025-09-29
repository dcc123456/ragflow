// src/components/CustomModal.tsx
import { Modal } from '@/components/ui/modal';
import { Layers, Users } from 'lucide-react';
import React, { useMemo } from 'react';
import { createRoot } from 'react-dom/client';
import UpgradeButton from './to-upgrade-button';

interface CustomModalProps {
  isOpen: boolean;
  onClose: () => void;
  type: 'dataset' | 'team-member';
  message: string;
}

const CustomModal: React.FC<CustomModalProps> = ({
  isOpen,
  onClose,
  type,
  message,
}) => {
  const title = useMemo(() => {
    return (
      <div className="mr-4">
        {/* Icon based on title */}
        {type === 'dataset' && (
          <Layers
            className="w-6 h-6 text-amber-600" /* ...svg attributes... */
          />
        )}
        {type === 'team-member' && <Users className="w-6 h-6 text-blue-500" />}
      </div>
    );
  }, [type]);
  const footer = useMemo(() => {
    return (
      <div className="flex items-center justify-end gap-2 text-xs">
        {type === 'dataset' && <div>Upgrade to get more storage</div>}
        {type === 'team-member' && <div>Upgrade to invite more</div>}
        <UpgradeButton />
      </div>
    );
  }, [type]);
  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      footer={footer}
      title={title}
      className="!w-[400px]"
    >
      <div className="flex items-start mb-4 flex-col gap-4 justify-start">
        <div className="text-start">{message}</div>
        <div className="w-full h-2 rounded-md bg-gradient-to-r from-red-600 to-red-600"></div>
      </div>
    </Modal>
  );
};

let currentModal: { destroy: () => void } | null = null;
interface IShowUpgradeTipsModalOptions {
  type: 'dataset' | 'team-member';
  message: string;
}
const showUpgradeTipsModal = ({
  type,
  message = '',
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
      type={type}
      message={message}
      onClose={closeModal}
    />,
  );

  currentModal = { destroy: closeModal };

  return currentModal;
};

export { showUpgradeTipsModal };

// use example
/**
 showUpgradeTipsModal({
      type: 'team-member',
      message: 'Your dataset is full (10 GB/10 GB). ',
    });
 */
