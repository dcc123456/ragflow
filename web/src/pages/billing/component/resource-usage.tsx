import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';
import {
  getBillingStorageCurrent,
  postBillingStorageSetTarget,
} from '@/services/price';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { camelCase } from 'lodash';
import { ArrowUpRight, DatabaseZap, LayoutGrid, Users } from 'lucide-react';
import React from 'react';
import { useTranslation } from 'react-i18next';
import { showAddOnManageModal } from './add-on-manage-modal';
import Process from './process';

interface CustomProgressProps {
  title: 'Apps' | 'Team Member' | 'Storage';
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

  const addOnManageOk = async ({ value }: { value: number }) => {
    if (addOnManageModal) {
      const url = window.location.href;
      const successUrl = `${url.split('?')[0]}?price-pay-status=success${url.split('?')[1] || ''}`;
      const errorUrl = `${url.split('?')[0]}?price-pay-status=cancel${url.split('?')[1] || ''}`;
      const { data } = await postBillingStorageSetTarget({
        tenant_id: tenantId,
        target_quantity_gb: value,
        session_cancel_url: errorUrl,
        session_success_url: successUrl,
      });
      const res = data?.data;
      if (res && res.redirect_to) {
        window.open(res.redirect_to);
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['getPlanOverview'] }),
        queryClient.invalidateQueries({ queryKey: ['getBaseOverview'] }),
        queryClient.invalidateQueries({ queryKey: ['billingStorageCurrent'] }),
      ]);
      addOnManageModal.destroy();
    }
  };
  const openAddOnManage = () => {
    const addOnCapacity = Math.max(0, limit - planValue);
    const currentStorage =
      storageCurrent?.effective_quantity_gb ?? addOnCapacity;
    const decreaseEffectiveAt = storageCurrent?.decrease_effective_at;
    addOnManageModal = showAddOnManageModal({
      defaultValue: currentStorage,
      onOk: addOnManageOk,
      price: storageCurrent?.unit_price || 0,
      decreaseEffectiveAt,
    });
  };

  const storageFooter = () => {
    if (title !== 'Storage') return null;
    return (
      <div className="flex justify-between items-end text-text-primary">
        <div>
          {planName} {t('billing.planUsed')}{' '}
          {value > planValue ? planValue : value}
          {unit}/{planValue}
          {unit}
        </div>
        <div className="flex items-end gap-3 cursor-pointer ">
          <span>
            {t('billing.addonUsed')} {value > planValue ? value - planValue : 0}
            {unit}/{parseFloat((limit - planValue).toFixed(2))}
            {unit}
          </span>
          <div
            className="flex items-center text-text-primary text-xs hover:outline outline-1 px-1 py-1 rounded-sm border border-border-button bg-bg-input "
            onClick={() => {
              openAddOnManage();
            }}
          >
            {t('billing.manage')}
            <ArrowUpRight size={12} />
          </div>
        </div>
      </div>
    );
  };
  return (
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
  );
};

export default ResourceUsage;
