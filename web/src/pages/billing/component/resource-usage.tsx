import { useCharge } from '@/pages/price/hook/use-price-hooks';
import { ArrowUpRight, DatabaseZap, LayoutGrid, Users } from 'lucide-react';
import React from 'react';
import { UsageBasedDeepDocPriceId } from '../contant';
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

  const addOnManageOk = async ({ value }: { value: number }) => {
    if (addOnManageModal) {
      const res = await checkout({
        price_id: UsageBasedDeepDocPriceId,
        quantity: value.toString(),
        payment_type: 'usage_based',
      });
      console.log('checkout', res);
      if (res && res.redirect_to) {
        window.open(res.redirect_to);
      }
      addOnManageModal.destroy();
    }
  };
  const openAddOnManage = () => {
    addOnManageModal = showAddOnManageModal({
      defaultValue: 40,
      onOk: addOnManageOk,
    });
  };

  const storageFooter = () => {
    if (title !== 'Storage') return null;
    return (
      <div className="flex justify-between items-end text-text-primary">
        <div>
          {planName} Plan used {value > planValue ? planValue : value}GB/
          {planValue}GB
        </div>
        <div className="flex items-end gap-3 cursor-pointer ">
          <span>
            Add-on used {value > planValue ? value - planValue : 0}GB/
            {limit - planValue}GB
          </span>
          <div
            className="flex items-center text-text-primary text-xs hover:outline outline-1 px-1 py-1 rounded-sm border border-border-button bg-bg-input "
            onClick={() => {
              openAddOnManage();
            }}
          >
            Manage
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
            {title}
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
