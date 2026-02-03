import { useCharge } from '@/pages/price/hook/use-price-hooks';
import { ArrowUpRight, DatabaseZap, LayoutGrid, Users } from 'lucide-react';
import React from 'react';
import { useTranslation } from 'react-i18next';
import { useBillingContext } from '..';
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
  planValue = 10,
  unit = '',
  children,
}) => {
  let addOnManageModal: { destroy: () => void };
  const { checkout } = useCharge();
  const { usageBasedPlans } = useBillingContext();
  const storageUsage = usageBasedPlans.find(
    (item) => item.name === title.toLowerCase(),
  );
  const { t } = useTranslation();

  const addOnManageOk = async ({ value }: { value: number }) => {
    if (addOnManageModal) {
      const res = await checkout({
        price_id: storageUsage?.price_ids || '',
        quantity: value.toString(),
        payment_type: 'usage_based',
      });
      if (res && res.redirect_to) {
        window.open(res.redirect_to);
      }
      addOnManageModal.destroy();
    }
  };
  const openAddOnManage = () => {
    const addOnCapacity = Math.max(0, limit - planValue);
    addOnManageModal = showAddOnManageModal({
      defaultValue: addOnCapacity,
      onOk: addOnManageOk,
      price: storageUsage?.price,
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
            {unit}/{limit - planValue}
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
            {t(`billing.${title.toLowerCase().replace(' ', '')}`)}
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
