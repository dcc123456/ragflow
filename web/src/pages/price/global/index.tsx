import PaymentStatusModal from '@/pages/billing/component/payment-status-modal';
import { isBillingEnabled } from '@/services/billingStatus';
import { ReactNode, createContext, useContext, useState } from 'react';
import { useFetchCurrentPlan } from '../hook/use-price-hooks';
import { FreeUpgradeModal } from '../price-modal/free-upgrade-modal';
import { ConfirmModal } from '../price-modal/price-confirm-modal';
import { PriceModalComponent } from '../price-modal/price-modal';
import { UpgradeTipsModal } from '../price-modal/upgrade-tips-modal';
import {
  CONFIRM_PRICE_EVENT,
  ConfirmPriceEventDetail,
  FREE_UPGRADE_TIPS_EVENT,
  FreeUpgradeTipsEventDetail,
  UPGRADE_TIPS_EVENT,
  UpgradeTipsEventDetail,
  useShowConfirmPriceModal,
  useShowFreeUpgradeTipsModal,
  useShowUpgradeTipsModal,
} from './hook';

interface UpgradeModalContextType {
  isModalOpen: boolean;
  openModal: (modalType?: string, modalProps?: any) => void;
  closeModal: () => void;
}

const UpgradeModalContext = createContext<UpgradeModalContextType | undefined>(
  undefined,
);

interface UpgradeModalProviderProps {
  children?: ReactNode;
}

export const showUpgradeTipsModal = (options?: UpgradeTipsEventDetail) => {
  const event = new CustomEvent(UPGRADE_TIPS_EVENT, { detail: options });
  window.dispatchEvent(event);
};

export const showPriceConfirmModal = (options?: ConfirmPriceEventDetail) => {
  const event = new CustomEvent(CONFIRM_PRICE_EVENT, { detail: options });
  window.dispatchEvent(event);
};

export const showFreeUpgradeTipsModal = (
  options?: FreeUpgradeTipsEventDetail,
) => {
  const event = new CustomEvent(FREE_UPGRADE_TIPS_EVENT, { detail: options });
  window.dispatchEvent(event);
};

export const UpgradeModalProvider: React.FC<UpgradeModalProviderProps> = ({
  children,
}) => {
  useFetchCurrentPlan();

  const [isModalOpen, setIsModalOpen] = useState(false);

  const openModal = () => {
    setIsModalOpen(true);
  };

  const closeModal = () => {
    setIsModalOpen(false);
  };

  const { upgradeTips, hideUpgradeTips } = useShowUpgradeTipsModal();

  const { confirmPrice, hideConfirmPrice } = useShowConfirmPriceModal();

  const { freeUpgradeTips, hideFreeUpgradeTips } =
    useShowFreeUpgradeTipsModal();

  return (
    <UpgradeModalContext.Provider
      value={{
        isModalOpen,
        openModal,
        closeModal,
      }}
    >
      {children}
      {isModalOpen && (
        <PriceModalComponent isOpen={isModalOpen} onClose={closeModal} />
      )}

      {upgradeTips.isOpen && upgradeTips.type && (
        <div className="fixed inset-0 z-50">
          <UpgradeTipsModal
            isOpen={upgradeTips.isOpen}
            type={upgradeTips.type}
            message={upgradeTips.message}
            container={upgradeTips.container}
            onClose={hideUpgradeTips}
          />
        </div>
      )}
      {freeUpgradeTips.isOpen && (
        <div className="fixed inset-0 z-50">
          <FreeUpgradeModal
            isOpen={freeUpgradeTips.isOpen}
            onClose={hideFreeUpgradeTips}
          />
        </div>
      )}

      {confirmPrice.isOpen && (
        <ConfirmModal
          plan={confirmPrice.plan}
          isOpen={confirmPrice.isOpen}
          onClose={hideConfirmPrice}
        />
      )}
      <PaymentStatusModal />
    </UpgradeModalContext.Provider>
  );
};

export const useUpgradeModal = () => {
  const context = useContext(UpgradeModalContext);
  if (!isBillingEnabled()) {
    return {} as UpgradeModalContextType;
  }
  if (context === undefined) {
    throw new Error(
      'useUpgradeModal must be used within an UpgradeModalProvider',
    );
  }
  return context as UpgradeModalContextType;
};
