import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import { pricePerGB } from '@/pages/price/config';
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
import {
  showAbandonPendingModal,
  showAddOnManageModal,
} from './add-on-manage-modal';
import BuyCreditsModal from './buy-points-modal';
import Process from './process';

interface CustomProgressProps {
  title: 'Apps' | 'Team Member' | 'Storage' | 'Document Parse';
  value: number;
  height?: number;
  limit: number;
  basicCapacity?: number;
  planName?: string;
  planValue?: number;
  unit?: string;
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
  children,
}) => {
  let addOnManageModal: { destroy: () => void };
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo?.tenant_id;
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { data: storageCurrent } = useQuery({
    queryKey: ['billingStorageCurrent', tenantId],
    enabled: title === 'Storage' && !!tenantId,
    queryFn: async () => {
      const { data: res } = await getBillingStorageCurrent(tenantId);
      if (res.code === 0) {
        return res.data;
      }
    },
  });

  const [isModalVisible, setIsModalVisible] = useState(false);

  const handleCancel = () => {
    setIsModalVisible(false);
  };

  const invalidateStorageQueries = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: ['getPlanOverview'] }),
      queryClient.invalidateQueries({ queryKey: ['getBaseOverview'] }),
      queryClient.invalidateQueries({ queryKey: ['billingStorageCurrent'] }),
    ]);

  const submitSetTarget = async (targetGb: number) => {
    const url = window.location.href;
    const successUrl = `${url.split('?')[0]}?price-pay-status=success${url.split('?')[1] || ''}`;
    const errorUrl = `${url.split('?')[0]}?price-pay-status=cancel${url.split('?')[1] || ''}`;
    const { data } = await postBillingStorageSetTarget({
      tenant_id: tenantId,
      target_quantity_gb: targetGb,
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
        pendingQuantityGb: res.pending_quantity_gb ?? 0,
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
      storageCurrent?.effective_quantity_gb ?? addOnCapacity;
    const decreaseEffectiveAt = storageCurrent?.decrease_effective_at;
    addOnManageModal = showAddOnManageModal({
      defaultValue: currentStorage,
      onOk: addOnManageOk,
      price: storageCurrent?.unit_price || pricePerGB,
      decreaseEffectiveAt,
    });
  };

  const openBuyPoints = () => {
    setIsModalVisible(true);
  };

  const storageFooter = () => {
    if (title == 'Storage' || title == 'Document Parse') {
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
                {title === 'Document Parse'
                  ? t('billing.creditsUsed')
                  : t('billing.addonUsed')}{' '}
                {value > planValue ? value - planValue : 0}
                {unit}/{parseFloat((limit - planValue).toFixed(2))}
                {unit}
              </span>
              <div
                className="flex items-center text-text-primary text-xs hover:outline outline-1 px-1 py-1 rounded-sm border border-border-button bg-bg-input "
                onClick={() => {
                  if (title === 'Storage') {
                    openAddOnManage();
                  } else {
                    openBuyPoints();
                  }
                }}
              >
                {title === 'Document Parse'
                  ? t('billing.buyCredits')
                  : t('billing.buyStorage')}
                <ArrowUpRight size={12} />
              </div>
            </div>
          )}
        </div>
      );
    } else {
      return null;
    }
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
          <span className="text-text-primary">{`${value}${unit}/${limit}${unit}`}</span>
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
        currentPoints={2000}
      />
    </>
  );
};

export default ResourceUsage;
