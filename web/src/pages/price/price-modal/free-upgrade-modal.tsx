// src/components/CustomModal.tsx
import { Modal } from '@/components/ui/modal/modal';
import React from 'react';
import UpgradeButton from './to-upgrade-button';

interface CustomModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const FreeUpgradeModal: React.FC<CustomModalProps> = ({
  isOpen,
  onClose,
}) => {
  return (
    <Modal
      open={isOpen}
      onCancel={onClose}
      showfooter={false}
      closable={false}
      maskClosable={true}
      className="!w-[500px]"
    >
      <div>
        <div className="flex items-center mb-4 flex-col justify-center">
          <div className="text-center text-3xl mb-8 mt-6 bg-gradient-to-r from-indigo-500 from-30% via-sky-500 via-60% to-cyan-400 bg-clip-text text-transparent">
            Love RAGFlow?
          </div>
          <div className="text-center text-xl text-text-primary">
            Join 50,000+ developers
          </div>
          <div className="text-center text-xl mb-10 text-text-primary">
            who’ve starred RAGFlow on GitHub!
          </div>
          <div className="flex items-center gap-6 mb-8">
            <a
              className="border border-border-default cursor-pointer py-1 px-2 rounded-sm whitespace-nowrap text-text-primary"
              href="https://github.com/infiniflow/ragflow"
              target="_blank"
              rel="noreferrer"
            >
              Star RAGFlow
            </a>
            <UpgradeButton text="Upgrade Plan" onCallBack={() => onClose()} />
          </div>
        </div>
        {/* width: calc(100% + 48px); transform: translateX(-24px) translateY(8px); */}
        <div className="flex items-center px-4 text-xs text-text-secondary w-[calc(100%+48px)] -translate-x-6 translate-y-2 text-start bg-accent-primary-5 h-9">
          With 🩵 from the RAGFlow team
        </div>
      </div>
    </Modal>
  );
};

// let currentModal: { destroy: () => void } | null = null;
// const showFreeUpgradeTipsModal = () => {
//   const rootElement = document.createElement('div');
//   document.body.appendChild(rootElement);

//   const reactRoot = createRoot(rootElement);
//   const closeModal = () => {
//     reactRoot.unmount();
//     document.body.removeChild(rootElement);
//     currentModal = null;
//   };

//   reactRoot.render(<CustomModal isOpen={true} onClose={closeModal} />);

//   currentModal = { destroy: closeModal };

//   return currentModal;
// };

// export { showFreeUpgradeTipsModal };

// use example
/**
 showUpgradeTipsModal({
      type: 'team-member',
      message: 'Your dataset is full (10 GB/10 GB). ',
    });
 */
