import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import { formatNumber } from '@/pages/admin/model-usage-statistics/utils';
import { useFetchAddonPlans } from '@/pages/price/hook/use-addon-plans';
import {
  getBillingStorageCurrent,
  postBillingStorageAbandonPending,
  postBillingStorageSetTarget,
} from '@/services/price';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { message } from 'antd';
import { camelCase } from 'lodash';
import {
  ArrowUpRight,
  Coins,
  DatabaseZap,
  LayoutGrid,
  Users,
} from 'lucide-react';
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { BillingQueryKey } from '../constants/query-keys';
import {
  showAbandonPendingModal,
  showAddOnManageModal,
} from './add-on-manage-modal';
import BuyCreditsModal from './buy-points-modal';
import Process from './process';

const BYTES_PER_GB = 1000 * 1000 * 1000;

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
  const planPointsRemaining = Math.max(
    0,
    (planPoints?.limit ?? 0) - (planPoints?.used ?? 0),
  );
  const addonPointsRemaining = Math.max(
    0,
    (addonPoints?.limit ?? 0) - (addonPoints?.used ?? 0),
  );
  const currentPoints = planPointsRemaining + addonPointsRemaining;
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

  const submitSetTarget = async (targetGb: number) => {
    const url = window.location.href;
    const successUrl = `${url.split('?')[0]}?price-pay-status=success${url.split('?')[1] || ''}`;
    const errorUrl = `${url.split('?')[0]}?price-pay-status=cancel${url.split('?')[1] || ''}`;
    const { data } = await postBillingStorageSetTarget({
      tenant_id: tenantId,
      target_quantity_bytes: Math.max(0, targetGb) * BYTES_PER_GB,
      session_cancel_url: errorUrl,
      session_success_url: successUrl,
    });
    return data;
  };

  const addOnManageOk = async ({ value }: { value: number }) => {
    if (!addOnManageModal) return;

    const data = await submitSetTarget(value);
    const res = data?.data;

    // Pending increase blocks the request — offer "Pay Now" or "Abandon & Apply".
    if (data?.code !== 0 && res?.can_abandon) {
      addOnManageModal.destroy();
      showAbandonPendingModal({
        pendingQuantityGb: Math.floor(
          (res.pending_quantity_bytes ?? 0) / BYTES_PER_GB,
        ),
        targetQuantityGb: value,
        invoiceUrl: res.invoice_url ?? '',
        onAbandon: async () => {
          await postBillingStorageAbandonPending({ tenant_id: tenantId });
          // Re-submit the original target now that the pending increase is gone.
          const retryData = await submitSetTarget(value);
          await invalidateStorageQueries();
          if (retryData?.code !== 0) {
            message.error(t('billing.storageUpgradeFailed'));
            return;
          }
          const retryRes = retryData?.data;
          if (retryRes?.redirect_to) {
            // Payment required — open Stripe payment page.
            window.open(retryRes.redirect_to);
          } else {
            // Auto-charged via default payment method — confirm to the user.
            message.success(t('billing.storageUpgradeSuccess', { value }));
          }
        },
      });
      return;
    }

    if (res?.redirect_to) {
      window.open(res.redirect_to);
    }
    await invalidateStorageQueries();
    addOnManageModal.destroy();
  };
  const openAddOnManage = () => {
    const addOnCapacity = Math.max(0, limit - planValue);
    const currentStorage =
      storageCurrent?.addon_storage_bytes != null
        ? Math.floor(storageCurrent.addon_storage_bytes / BYTES_PER_GB)
        : addOnCapacity;
    const decreaseEffectiveAt = storageCurrent?.decrease_effective_at;
    addOnManageModal = showAddOnManageModal({
      defaultValue: currentStorage,
      onOk: addOnManageOk,
      price: storageCurrent?.unit_price || pricePerGBFromApi,
      decreaseEffectiveAt,
    });
  };

  const openBuyPoints = () => {
    setIsModalVisible(true);
  };

  const storageFooter = () => {
    if (title === 'Storage') {
      return (
        <div className="flex justify-between items-end text-text-primary">
          <div>
            {planName} {t('billing.planUsed')}{' '}
            {value > planValue ? planValue : value}
            {unit}/{planValue}
            {unit}
          </div>
          {!(planName == 'Free Plan' || planName == 'Free') && (
            <div className="flex items-end gap-3 cursor-pointer ">
              <span>
                {t('billing.addonUsed')}{' '}
                {(value > planValue ? value - planValue : 0).toFixed(2)}
                {unit}/{parseFloat((limit - planValue).toFixed(2))}
                {unit}
              </span>
              {isStorageCurrentLoading ? (
                <div className="flex items-center text-xs px-1 py-1 rounded-sm border border-border-button bg-bg-input text-text-secondary cursor-not-allowed">
                  {t('common.loading', 'Loading...')}
                </div>
              ) : !isStorageCurrentError &&
                storageCurrent?.payment_required &&
                storageCurrent?.payment_recovery_url ? (
                <a
                  href={storageCurrent.payment_recovery_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center text-xs hover:outline outline-1 px-1 py-1 rounded-sm border border-red-400 bg-bg-input text-red-500"
                >
                  {t('billing.payStorageInvoice', 'Pay Invoice')}
                  <ArrowUpRight size={12} />
                </a>
              ) : (
                <div
                  className="flex items-center text-text-primary text-xs hover:outline outline-1 px-1 py-1 rounded-sm border border-border-button bg-bg-input "
                  onClick={() => openAddOnManage()}
                >
                  {t('billing.buyStorage')}
                  <ArrowUpRight size={12} />
                </div>
              )}
            </div>
          )}
        </div>
      );
    }
    if (title === 'Document Parse' && (planPoints || addonPoints)) {
      return (
        <div className="flex gap-2 text-text-primary justify-between">
          {/* Plan quota row */}
          <div className="flex justify-between items-center flex-col">
            <span>
              {planName} {t('billing.planUsed')}
            </span>
            <span>
              {planPoints?.used ?? 0}/{planPoints?.limit ?? 0} pts
            </span>
          </div>
          {/* Addon row */}
          <div className="flex justify-between items-center flex-col">
            <span>{t('billing.creditsUsed') || 'Addon Points'}</span>
            <span>
              {addonPoints?.used ?? 0}/{addonPoints?.limit ?? 0} pts
            </span>
          </div>

          {!(planName == 'Free Plan' || planName == 'Free') && (
            <div
              className="flex items-center justify-center text-text-primary text-xs hover:outline outline-1 px-1 py-1 rounded-sm border border-border-button bg-bg-input cursor-pointer mt-1"
              onClick={() => openBuyPoints()}
            >
              {t('billing.buyCredits')}
              <ArrowUpRight size={12} />
            </div>
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
            ) : (
              <>
                {showValue && <span>{`${value}${unit}`}/</span>}
                <span>{`${formatNumber(limit)}${unit}`}</span>
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
