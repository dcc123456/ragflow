import { nextLayoutRef } from '@/layouts/root-layout';
import { isBillingEnabled } from '@/services/billingStatus';
import storagePrivate from '@/utils/authorization-private-util';
import { useCallback, useEffect, useState } from 'react';
import { useLocation } from 'react-router';
import { showFreeUpgradeTipsModal, showUpgradeTipsModal } from '.';
import { freePageNumber } from '../config';
import { PriceName } from '../constant';
import { IConfirmPlan, ICurrentPlan, IPricePlan } from '../interface';

export const UPGRADE_TIPS_EVENT = 'SHOW_UPGRADE_TIPS';
export const CONFIRM_PRICE_EVENT = 'SHOW_CONFIRM_PRICE_TIPS';
export const FREE_UPGRADE_TIPS_EVENT = 'SHOW_FREE_UPGRADE_TIPS';

export interface ConfirmPriceEventDetail {
  plan: IConfirmPlan;
  container?: HTMLElement;
}

export interface UpgradeTipsEventDetail {
  type: 'storage' | 'team-member' | 'apps' | 'points' | 'points';
  message: string;
  container?: HTMLElement;
}

export interface FreeUpgradeTipsEventDetail {
  container?: HTMLElement;
}

/**
 * Frontend error codes for billing resource insufficient errors.
 * Must stay in sync with RetCode.BILLING_* in common/constants.py
 */
export enum PriceCode {
  MultLimit = 2000,
  AppsLimit = 2001,
  SeatsLimit = 2002,
  StorageLimit = 2003,
  PointsLimit = 2004,
}

export const RESOURCE_INSUFFICIENT_PRICE_CODES = new Set<number>([
  PriceCode.MultLimit,
  PriceCode.AppsLimit,
  PriceCode.SeatsLimit,
  PriceCode.StorageLimit,
  PriceCode.PointsLimit,
]);

export const isResourceInsufficientPriceCode = (code?: number) =>
  typeof code === 'number' && RESOURCE_INSUFFICIENT_PRICE_CODES.has(code);

interface IPriceData {
  code: PriceCode;
  detail: { current: number; limit: number };
}
export const showPriceModal = ({ code, detail }: IPriceData) => {
  switch (code) {
    case PriceCode.MultLimit:
      return true;
    case PriceCode.SeatsLimit:
      showUpgradeTipsModal({
        type: 'team-member',
        message: `You've reached your team member count limit for your plan (${detail.current}/${detail.limit}). `,
        container: nextLayoutRef.current || undefined,
      });
      return true;
    case PriceCode.AppsLimit:
      showUpgradeTipsModal({
        type: 'apps',
        message: `You've reached your app count limit for your plan (${detail.current}/${detail.limit}). `,
        container: nextLayoutRef.current || undefined,
      });
      return true;
    case PriceCode.StorageLimit:
      showUpgradeTipsModal({
        type: 'storage',
        message: `You've reached your storage limit for your plan (${detail.current}GB/${detail.limit} GB}). `,
        container: nextLayoutRef.current || undefined,
      });
      return true;
    case PriceCode.PointsLimit:
      showUpgradeTipsModal({
        type: 'points',
        message: `Your points balance is insufficient. `,
        container: nextLayoutRef.current || undefined,
      });
      return true;
    default:
      return false;
  }
};

export const useShowUpgradeTipsModal = () => {
  const [upgradeTips, setUpgradeTips] = useState<{
    isOpen: boolean;
    type: 'storage' | 'team-member' | 'apps' | 'points' | null;
    message: string;
    container?: HTMLElement;
  }>({
    isOpen: false,
    type: null,
    message: '',
    container: undefined,
  });

  const showUpgradeTips = useCallback(
    (options: {
      type: 'storage' | 'team-member' | 'apps' | 'points' | 'points';
      message: string;
      container?: HTMLElement;
    }) => {
      setUpgradeTips({
        isOpen: true,
        type: options.type,
        message: options.message,
        container: options.container,
      });
    },
    [],
  );

  const hideUpgradeTips = () => {
    setUpgradeTips({
      isOpen: false,
      type: null,
      message: '',
      container: undefined,
    });
  };

  useEffect(() => {
    const handleUpgradeTipsEvent = (event: Event) => {
      const customEvent = event as CustomEvent<UpgradeTipsEventDetail>;
      showUpgradeTips(customEvent.detail);
    };

    window.addEventListener(UPGRADE_TIPS_EVENT, handleUpgradeTipsEvent);
    return () => {
      window.removeEventListener(UPGRADE_TIPS_EVENT, handleUpgradeTipsEvent);
    };
  }, [showUpgradeTips]);

  useEffect(() => {
    if (!upgradeTips.isOpen) {
      const timer = setTimeout(() => {
        setUpgradeTips((prev) => ({
          ...prev,
          type: null,
          message: '',
          container: undefined,
        }));
      }, 300);

      return () => clearTimeout(timer);
    }
  }, [upgradeTips.isOpen, setUpgradeTips]);

  return {
    upgradeTips,
    setUpgradeTips,
    showUpgradeTips,
    hideUpgradeTips,
  };
};

export const useShowFreeUpgradeTipsModal = () => {
  const [freeUpgradeTips, setFreeUpgradeTips] = useState<{
    isOpen: boolean;
    container?: HTMLElement;
  }>({
    isOpen: false,
    container: undefined,
  });

  const showFreeUpgradeTips = useCallback(
    (options: { container?: HTMLElement }) => {
      setFreeUpgradeTips({
        isOpen: true,
        container: options?.container,
      });
    },
    [setFreeUpgradeTips],
  );

  const hideFreeUpgradeTips = () => {
    setFreeUpgradeTips({
      isOpen: false,
      container: undefined,
    });
  };

  useEffect(() => {
    const handleFreeUpgradeTipsEvent = (event: Event) => {
      const customEvent = event as CustomEvent<FreeUpgradeTipsEventDetail>;
      showFreeUpgradeTips(customEvent.detail);
    };

    window.addEventListener(
      FREE_UPGRADE_TIPS_EVENT,
      handleFreeUpgradeTipsEvent,
    );
    return () => {
      window.removeEventListener(
        FREE_UPGRADE_TIPS_EVENT,
        handleFreeUpgradeTipsEvent,
      );
    };
  }, [showFreeUpgradeTips]);

  useEffect(() => {
    if (!freeUpgradeTips.isOpen) {
      const timer = setTimeout(() => {
        setFreeUpgradeTips((prev) => ({
          ...prev,
          container: undefined,
        }));
      }, 300);

      return () => clearTimeout(timer);
    }
  }, [freeUpgradeTips.isOpen, setFreeUpgradeTips]);

  return {
    freeUpgradeTips,
    setFreeUpgradeTips,
    showFreeUpgradeTips,
    hideFreeUpgradeTips,
  };
};

export const useShowConfirmPriceModal = () => {
  const [confirmPrice, setConfirmPrice] = useState<{
    isOpen: boolean;
    plan: IPricePlan;
    container?: HTMLElement;
  }>({
    isOpen: false,
    plan: {
      id: '',
      description: '',
      title: '',
      price: '0',
      feature: {
        apps: '0',
        teamMembers: '0',
        datasetStorage: '0',
        apiRequests: '0',
      },
    },
    container: undefined,
  });

  const showConfirmPrice = useCallback(
    (options: { plan: IPricePlan; container?: HTMLElement }) => {
      setConfirmPrice({
        isOpen: true,
        plan: options.plan,
        container: options.container,
      });
    },
    [],
  );

  const hideConfirmPrice = () => {
    setConfirmPrice({
      isOpen: false,
      plan: {
        id: '',
        description: '',
        title: '',
        price: '0',
        feature: {
          apps: '0',
          teamMembers: '0',
          datasetStorage: '0',
          apiRequests: '0',
        },
      },
      container: undefined,
    });
  };

  useEffect(() => {
    const handleConfirmPriceEvent = (event: Event) => {
      const customEvent = event as CustomEvent<ConfirmPriceEventDetail>;
      showConfirmPrice(customEvent.detail);
    };

    window.addEventListener(CONFIRM_PRICE_EVENT, handleConfirmPriceEvent);
    return () => {
      window.removeEventListener(CONFIRM_PRICE_EVENT, handleConfirmPriceEvent);
    };
  }, [showConfirmPrice]);

  useEffect(() => {
    if (!confirmPrice.isOpen) {
      const timer = setTimeout(() => {
        setConfirmPrice((prev) => ({
          ...prev,
          type: null,
          message: '',
          container: undefined,
        }));
      }, 300);

      return () => clearTimeout(timer);
    }
  }, [confirmPrice.isOpen, setConfirmPrice]);
  return {
    confirmPrice,
    setConfirmPrice,
    showConfirmPrice,
    hideConfirmPrice,
  };
};

export const useComputedRouterChangeCount = () => {
  const { pathname } = useLocation();
  const run = useCallback(() => {
    const plan: ICurrentPlan = storagePrivate.getPricePlan();
    const whitelist = ['/login', '/register', '/billing', '/price'];
    if (whitelist.some((item) => pathname.includes(item))) {
      return;
    }
    if (plan && plan.plan_name !== PriceName.Trial) {
      return;
    }
    const countStr = localStorage.getItem('pageViewCount');
    let count = countStr ? parseInt(countStr, 10) : 0;
    count++;
    localStorage.setItem('pageViewCount', count.toString());

    if (count > freePageNumber) {
      showFreeUpgradeTipsModal({
        container: nextLayoutRef?.current || undefined,
      });
      localStorage.setItem('pageViewCount', '0');
    }
  }, [pathname]);

  useEffect(() => {
    if (isBillingEnabled()) {
      run();
    }
  }, [pathname, run]);
};
