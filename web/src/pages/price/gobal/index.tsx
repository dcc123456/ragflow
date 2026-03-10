import { nextLayoutRef } from '@/layouts/root-layout';
import storagePrivate from '@/utils/authorization-private-util';
import {
  ReactNode,
  createContext,
  useContext,
  useEffect,
  useState,
} from 'react';
import { freePageNumber } from '../config';
import { PriceName } from '../constant';
// import { useFetchCurrentPlan } from '../hook/use-price-hooks';
import { useFetchCurrentPlan } from '../hook/use-price-hooks';
import { ICurrentPlan } from '../interface';
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
  children: ReactNode;
}

export const showUpgradeTipsModal = (options?: UpgradeTipsEventDetail) => {
  const event = new CustomEvent(UPGRADE_TIPS_EVENT, { detail: options });
  window.dispatchEvent(event);
};

export const showPriceComfirmModal = (options?: ConfirmPriceEventDetail) => {
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
  if (import.meta.env.VITE_BILLING_ENABLED !== '1') {
    return <>{children}</>;
  }
  const location = window.location.pathname.toLowerCase();
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

  useEffect(() => {
    const plan: ICurrentPlan = storagePrivate.getPricePlan();
    if (plan && plan.plan_name !== PriceName.Trial) {
      return;
    }
    const countStr = localStorage.getItem('pageViewCount');
    let count = countStr ? parseInt(countStr, 10) : 0;
    count++;
    localStorage.setItem('pageViewCount', count.toString());

    if (count > freePageNumber) {
      // Show upgrade tips
      showFreeUpgradeTipsModal({
        container: nextLayoutRef?.current || undefined,
      });
      localStorage.setItem('pageViewCount', '0');
    }
  }, [location]);

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
      {/* {JSON.stringify(freeUpgradeTips)} */}
      {freeUpgradeTips.isOpen && (
        <div className="fixed inset-0 z-50">
          <FreeUpgradeModal
            isOpen={freeUpgradeTips.isOpen}
            // isOpen={true}
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
    </UpgradeModalContext.Provider>
  );
};

export const useUpgradeModal = () => {
  const context = useContext(UpgradeModalContext);
  if (context === undefined) {
    throw new Error(
      'useUpgradeModal must be used within an UpgradeModalProvider',
    );
  }
  return context;
};
