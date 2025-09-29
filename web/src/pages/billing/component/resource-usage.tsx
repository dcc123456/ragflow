import { ArrowUpRight, Layers, LayoutGrid, Users } from 'lucide-react';
import React from 'react';
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
  const addOnManageOk = (e) => {
    console.log(e);
    if (addOnManageModal) {
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
      <div className="flex justify-between items-end text-muted-foreground">
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
            className="flex items-center text-foreground text-xl hover:outline outline-1 rounded-sm p-1 "
            onClick={() => {
              openAddOnManage();
            }}
          >
            Manage
            <ArrowUpRight />
          </div>
        </div>
      </div>
    );
  };
  return (
    <div className="bg-background-card p-4 rounded mb-4">
      <div className="flex justify-between items-center mb-2">
        <div className="flex items-center">
          <span className="mr-2">
            {/* icon */}
            {title === 'Apps' && (
              <div className="border rounded-sm p-1">
                <LayoutGrid />
              </div>
            )}
            {title === 'Team Member' && (
              <div className="border rounded-sm p-1">
                <Users />
              </div>
            )}
            {title === 'Storage' && (
              <div className="border rounded-sm p-1">
                <Layers />
              </div>
            )}
          </span>
          <span className="text-white text-lg font-semibold">{title}</span>
        </div>
        <span className="text-white">{`${value}${unit}/${limit}${unit}`}</span>
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
