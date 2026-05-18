// src/components/CustomModal.tsx
import { Modal } from '@/components/ui/modal/modal';
import { Coins, DatabaseZap, LayoutGrid, Users } from 'lucide-react';
import React, { useMemo } from 'react';
import { createPortal } from 'react-dom';
import { createRoot } from 'react-dom/client';
import UpgradeButton from './to-upgrade-button';

interface CustomModalProps {
  isOpen: boolean;
  onClose: () => void;
  type: 'dataset' | 'team-member' | 'apps' | 'points';
  message: string;
  container?: HTMLElement;
}

export const UpgradeTipsModal: React.FC<CustomModalProps> = ({
  isOpen,
  onClose,
  type,
  message,
  container,
}) => {
  const title = useMemo(() => {
    return (
      <div className="mr-4">
        {/* Icon based on title */}
        {type === 'apps' && (
          <LayoutGrid
            className="w-6 h-6 text-text-primary" /* ...svg attributes... */
          />
        )}
        {type === 'dataset' && (
          <DatabaseZap
            className="w-6 h-6 text-text-primary" /* ...svg attributes... */
          />
        )}
        {type === 'team-member' && (
          <Users className="w-6 h-6 text-text-primary" />
        )}
        {type === 'points' && <Coins className="w-6 h-6 text-text-primary" />}
      </div>
    );
  }, [type]);
  const footer = useMemo(() => {
    return (
      <div className="flex items-center justify-end gap-2 text-xs">
        {type === 'apps' && <div>Upgrade to get more apps</div>}
        {type === 'dataset' && <div>Upgrade to get more storage</div>}
        {type === 'team-member' && <div>Upgrade to invite more</div>}
        {type === 'points' && <div>Upgrade to get more points</div>}
        <UpgradeButton isModal={true} onCallBack={onClose} />
      </div>
    );
  }, [type, onClose]);
  const modalContent = (
    <Modal
      open={isOpen}
      onCancel={onClose}
      footer={footer}
      title={title}
      className="!w-[400px]"
    >
      <div className="flex items-start mb-4 flex-col gap-4 justify-start">
        <div className="text-start">{message}</div>
        <div className="w-full h-2 rounded-md bg-state-error"></div>
      </div>
    </Modal>
  );
  console.log('modalContent', modalContent);
  if (container) {
    return createPortal(modalContent, container);
  }

  return modalContent;
};

let currentModal: { destroy: () => void } | null = null;
interface IShowUpgradeTipsModalOptions {
  type: 'dataset' | 'team-member' | 'apps' | 'points';
  message: string;
  container?: HTMLElement;
}
const showUpgradeTipsModal = ({
  type,
  message = '',
  container,
}: IShowUpgradeTipsModalOptions) => {
  const rootElement = document.createElement('div');

  if (container) {
    container.appendChild(rootElement);
  } else {
    document.body.appendChild(rootElement);
  }

  const reactRoot = createRoot(rootElement);
  const closeModal = () => {
    reactRoot.unmount();
    if (container) {
      container.removeChild(rootElement);
    } else {
      document.body.removeChild(rootElement);
    }
    currentModal = null;
  };

  reactRoot.render(
    <UpgradeTipsModal
      isOpen={true}
      type={type}
      message={message}
      onClose={closeModal}
      container={container}
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
