import message from '@/components/ui/message';
import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import { formatNumber } from '@/pages/admin/model-usage-statistics/utils';
import { useFetchAddonPlans } from '@/pages/price/hook/use-addon-plans';
import {
  BillingDirectCheckoutResultEvent,
  StorageAddonResultKey,
} from '@/pages/price/hook/use-price-hooks';
import billingService, {
  getBillingStorageCurrent,
  postBillingStorageSetTarget,
} from '@/services/price';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { camelCase } from 'lodash';
import { Coins, DatabaseZap, LayoutGrid, Users } from 'lucide-react';
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { BillingQueryKey } from '../constants/query-keys';
import { showAddOnManageModal } from './add-on-manage-modal';
import BuyCreditsModal from './buy-points-modal';
import Process from './process';

const BYTES_PER_GB = 1000 * 1000 * 1000;

const formatStorageInGb = (value: number) => {
  const roundedValue = new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 2,
  }).format(value);
  return `${roundedValue} GB`;
};

interface CustomProgressProps {
  title: 'Apps' | 'Team Member' | 'Storage' | 'Document Parse';
  value: number;
  height?: number;
  limit: number;
  basicCapacity?: number;
  planName?: string;
  planValue?: number;
  unit?: string;
  showValue?: boolean;
  planPoints?: { used: number; limit: number; unit?: string };
  addonPoints?: { used: number; limit: number; unit?: string };
  children?: React.ReactNode;
}

const ResourceUsage: React.FC<CustomProgressProps> = ({
  title,
  value,
  limit,
  height = 8,
  basicCapacity,
  planName = 'Free',
  planValue = 0,
  unit = '',
  showValue = true,
  planPoints,
  addonPoints,
  children,
}) => {
  let addOnManageModal: { destroy: () => void };
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const currentPoints = limit - value;
  const { pricePerGB: pricePerGBFromApi } = useFetchAddonPlans();
  const {
    data: storageCurrent,
    isLoading: isStorageCurrentLoading,
    isError: isStorageCurrentError,
  } = useQuery({
    queryKey: [BillingQueryKey.StorageCurrent, tenantId],
    enabled: title === 'Storage' && !!tenantId,
    queryFn: async () => {
      const { data: res } = await getBillingStorageCurrent(tenantId);
      if (res.code === 0) {
        return res.data;
      }
      throw new Error(res.message || 'Failed to fetch storage current');
    },
  });

  const [isModalVisible, setIsModalVisible] = useState(false);

  const handleCancel = () => {
    setIsModalVisible(false);
  };

  const invalidateStorageQueries = () =>
    Promise.all([
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.PlanOverview],
      }),
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.BaseOverview],
      }),
      queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.StorageCurrent],
      }),
    ]);

  const submitSetTarget = async (targetGb: number, setupIntentId?: string) => {
    const currentUrl = new URL(window.location.href);
    const successUrl = new URL(currentUrl.toString());
    const errorUrl = new URL(currentUrl.toString());
    successUrl.searchParams.set('price-pay-status', 'success');
    errorUrl.searchParams.set('price-pay-status', 'cancel');
    const { data } = await postBillingStorageSetTarget({
      tenant_id: tenantId,
      target_storage_bytes: Math.max(0, targetGb) * BYTES_PER_GB,
      session_cancel_url: errorUrl.toString(),
      session_success_url: successUrl.toString(),
      setup_intent_id: setupIntentId,
    });
    return data;
  };

  const addOnManageOk = async ({
    value,
    setupIntentId,
  }: {
    value: number;
    paymentMethodReady?: boolean;
    setupIntentId?: string;
  }) => {
    if (!addOnManageModal) return;

    const data = await submitSetTarget(value, setupIntentId);
    const res = data?.data;

    if (data?.code !== 0) {
      message.error(t('billing.storageUpgradeFailed'));
      return;
    }

    // Use payment_state to determine flow: paid -> modal, requires_action -> redirect
    // Also handle requires_payment_method_setup case which has no payment_state
    if (
      (res?.payment_state === 'requires_action' ||
        res?.requires_payment_method_setup) &&
      res?.redirect_to
    ) {
      window.open(res.redirect_to);
    } else if (res?.payment_state === 'paid') {
      // Publish success event for in-app modal
      const payload = {
        status: 'paid' as const,
        amount: res?.amount_cents ? res.amount_cents / 100 : undefined,
        currency: res?.currency,
        invoice_id: res?.invoice_id,
        invoice_url: res?.invoice_url,
        invoice_pdf_url: res?.invoice_pdf_url,
        storage_gb: value,
        product_type: 'storage' as const,
      };
      sessionStorage.setItem(StorageAddonResultKey, JSON.stringify(payload));
      window.dispatchEvent(
        new CustomEvent(BillingDirectCheckoutResultEvent, { detail: payload }),
      );
    }
    await invalidateStorageQueries();
  };

  const previewImmediateStorageCharge = async (targetGb: number) => {
    const { data: upcoming } = await billingService.getUpcoming({
      tenant_id: tenantId,
      target_storage_bytes: Math.max(0, targetGb) * BYTES_PER_GB,
    });

    if (upcoming?.code === 0) {
      return upcoming?.data;
    }

    return undefined;
  };

  const openAddOnManage = () => {
    const addOnCapacity = Math.max(0, limit - planValue);
    const currentStorage =
      storageCurrent?.addon_storage_bytes != null
        ? Math.floor(storageCurrent.addon_storage_bytes / BYTES_PER_GB)
        : addOnCapacity;
    addOnManageModal = showAddOnManageModal({
      tenantId,
      defaultValue: currentStorage,
      currentValue: currentStorage,
      onOk: addOnManageOk,
      price: storageCurrent?.unit_price || pricePerGBFromApi,
      getUpgradePreview: previewImmediateStorageCharge,
    });
  };

  const openBuyPoints = () => {
    setIsModalVisible(true);
  };

  const storageFooter = () => {
    if (title === 'Storage') {
      return (
        <div className="flex justify-between items-center text-text-primary">
          <div className="flex flex-col items-start">
            <span>
              {planName} {t('billing.planUsed')}
            </span>
            {value > planValue
              ? formatStorageInGb(planValue)
              : formatStorageInGb(value)}{' '}
            /{formatStorageInGb(planValue)}
          </div>
          {!(planName == 'Free Plan' || planName == 'Free') && (
            <>
              <div className="flex flex-col items-start">
                <span>{t('billing.addonUsed')} </span>
                <span>
                  {formatStorageInGb(value > planValue ? value - planValue : 0)}
                  /{formatStorageInGb(limit - planValue)}
                </span>
              </div>
              {isStorageCurrentLoading ? (
                <div className="flex items-center text-sm px-1 py-1 rounded-sm border border-border-button bg-bg-input text-text-secondary cursor-not-allowed">
                  {t('common.loading', 'Loading...')}
                </div>
              ) : !isStorageCurrentError &&
                storageCurrent?.payment_required &&
                storageCurrent?.payment_recovery_url ? (
                <a
                  href={storageCurrent.payment_recovery_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center text-sm hover:outline outline-1 px-1 py-1 rounded-sm border border-red-400 bg-bg-input text-red-500"
                >
                  {t('billing.payStorageInvoice', 'Pay Invoice')}
                  {/* <ArrowUpRight size={12} /> */}
                </a>
              ) : (
                <div
                  className="flex items-center justify-center text-text-primary text-sm hover:outline outline-1 px-1 py-1 rounded-sm border border-border-button bg-bg-input cursor-pointer mt-1"
                  onClick={() => openAddOnManage()}
                >
                  {t('billing.buyStorage')}
                  {/* <ArrowUpRight size={12} /> */}
                </div>
              )}
            </>
          )}
        </div>
      );
    }
    if (title === 'Document Parse') {
      return (
        <div className="flex gap-2 text-text-primary justify-between items-center">
          {/* Plan quota row */}
          <div className="flex justify-between items-start flex-col">
            <span>
              {planName} {t('billing.planUsed')}
            </span>
            <span>
              {value > planValue
                ? formatNumber(planValue)
                : formatNumber(value)}{' '}
              /{formatNumber(planValue)} pts
              {/* {formatNumber(planPoints?.used ?? 0)}/
              {formatNumber(planPoints?.limit ?? 0)} pts */}
            </span>
          </div>
          {/* Addon row */}
          {/* {addonPoints?.limit !== 0 && (
            <div className="flex justify-between items-start flex-col">
              <span>{t('billing.creditsUsed') || 'Addon Points'}</span>
              <span>
                {formatNumber(value > planValue ? value - planValue : 0)}/
                {formatNumber(limit - planValue)} pts
              </span>
            </div>
          )} */}
          {!(planName == 'Free Plan' || planName == 'Free') && (
            <>
              <div className="flex justify-between items-start flex-col">
                <span>{t('billing.creditsUsed') || 'Addon Points'}</span>
                <span>
                  {/* {formatNumber(addonPoints?.used ?? 0)}/
                {formatNumber(addonPoints?.limit ?? 0)} pts */}
                  {formatNumber(value > planValue ? value - planValue : 0)}/
                  {formatNumber(limit - planValue)} pts
                </span>
              </div>
              <div
                className="flex items-center justify-center text-text-primary text-sm hover:outline outline-1 px-1 py-1 rounded-sm border border-border-button bg-bg-input cursor-pointer mt-1"
                onClick={() => openBuyPoints()}
              >
                {t('billing.buyCredits')}
                {/* <ArrowUpRight size={12} /> */}
              </div>
            </>
          )}
        </div>
      );
    }
    return null;
  };
  return (
    <>
      <div className="bg-bg-input border border-border-default p-4 rounded mb-4">
        <div className="flex justify-between items-center mb-2">
          <div className="flex items-center">
            <span className="mr-2">
              {/* icon */}
              {title === 'Apps' && (
                <div className=" rounded-sm p-1">
                  <LayoutGrid size={16} />
                </div>
              )}
              {title === 'Team Member' && (
                <div className=" rounded-sm p-1">
                  <Users size={16} />
                </div>
              )}
              {title === 'Storage' && (
                <div className=" rounded-sm p-1">
                  <DatabaseZap size={16} />
                </div>
              )}
              {title === 'Document Parse' && (
                <div className=" rounded-sm p-1">
                  <Coins size={16} />
                </div>
              )}
            </span>
            <span className="text-text-primary text-base font-normal">
              {t(`billing.${camelCase(title).replace(' ', '')}`)}
            </span>
          </div>
          <div className="text-text-primary">
            {title === 'Document Parse' && (planPoints || addonPoints) ? (
              <span>{`${formatNumber(currentPoints)} ${unit}`}</span>
            ) : title === 'Storage' ? (
              <>
                {showValue && <span>{`${formatStorageInGb(value)}`}/</span>}
                <span>{`${formatStorageInGb(limit)}`}</span>
              </>
            ) : (
              <>
                {showValue && <span>{`${value} ${unit}`}/</span>}
                <span>{`${formatNumber(limit)} ${unit}`}</span>
              </>
            )}
          </div>
        </div>
        <Process
          value={value}
          limit={limit}
          basicCapacity={basicCapacity}
          height={height}
        ></Process>
        {storageFooter()}
        {children}
      </div>
      <BuyCreditsModal
        visible={isModalVisible}
        onClose={handleCancel}
        currentPoints={currentPoints}
      />
    </>
  );
};

export default ResourceUsage;
