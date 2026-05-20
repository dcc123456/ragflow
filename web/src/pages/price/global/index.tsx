import { StripeProvider } from '@/contexts/stripe-context';
import PaymentStatusModal from '@/pages/billing/component/payment-status-modal';
import { isBillingEnabled } from '@/services/billingStatus';
import { ReactNode, createContext, useContext, useState } from 'react';
import { useFetchCurrentPlan } from '../hook/use-price-hooks';
import { FreeUpgradeModal } from '../price-modal/free-upgrade-modal';
import { ConfirmModal } from '../price-modal/price-confirm-modal';
import { PriceModalComponent } from '../price-modal/price-modal';
import { UpgradeTipsModal } from '../price-modal/upgrade-tips-modal';
import {
  showFreeUpgradeTipsModal,
  showPriceConfirmModal,
  showUpgradeTipsModal,
  useShowConfirmPriceModal,
  useShowFreeUpgradeTipsModal,
  useShowUpgradeTipsModal,
} from './hook';

export {
  showFreeUpgradeTipsModal,
  showPriceConfirmModal,
  showUpgradeTipsModal,
};

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
        <StripeProvider
          publishableKey={confirmPrice.stripe_publishable_key ?? null}
        >
          <ConfirmModal
            plan={confirmPrice.plan}
            isOpen={confirmPrice.isOpen}
            onClose={hideConfirmPrice}
            has_reusable_payment_method={
              confirmPrice.has_reusable_payment_method
            }
          />
        </StripeProvider>
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
