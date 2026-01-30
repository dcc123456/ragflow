import { nextLayoutRef } from '@/layouts/next';
import { useCallback, useEffect, useState } from 'react';
import { showUpgradeTipsModal } from '.';
import { IConfirmPlan, IPricePlan } from '../interface';

export const UPGRADE_TIPS_EVENT = 'SHOW_UPGRADE_TIPS';
export const CONFIRM_PRICE_EVENT = 'SHOW_CONFIRM_PRICE_TIPS';
export const FREE_UPGRADE_TIPS_EVENT = 'SHOW_FREE_UPGRADE_TIPS';

export interface ConfirmPriceEventDetail {
  plan: IConfirmPlan;
  container?: HTMLElement;
}

export interface UpgradeTipsEventDetail {
  type: 'dataset' | 'team-member' | 'apps';
  message: string;
  container?: HTMLElement;
}

export interface FreeUpgradeTipsEventDetail {
  container?: HTMLElement;
}

export enum PriceCode {
  MultLimit = 600,
  SeatsLimit = 601,
  AppsLimit = 602,
  StorageLimit = 603,
}

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
        message: `Your dataset is full (${detail.current}/${detail.limit}). `,
        container: nextLayoutRef.current || undefined,
      });
      return true;
    case PriceCode.AppsLimit:
      showUpgradeTipsModal({
        type: 'apps',
        message: `Your app is full (${detail.current}/${detail.limit}). `,
        container: nextLayoutRef.current || undefined,
      });
      return true;
    case PriceCode.StorageLimit:
      showUpgradeTipsModal({
        type: 'dataset',
        message: `Your dataset is full (${detail.current}GB/${detail.limit} GB}). `,
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
    type: 'dataset' | 'team-member' | 'apps' | null;
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
      type: 'dataset' | 'team-member' | 'apps';
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
