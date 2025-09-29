import classNames from 'classnames';
import { GitPullRequestArrow, Layers, LayoutGrid, Users } from 'lucide-react';
import React from 'react';
import { showPriceComfirmModal } from '.';
import { IFeatureProps, IPricePlanWithButton } from '../interface';
import './index.less';

interface ISuffixProps {
  id: number;
  icon: JSX.Element;
  text: 'apps' | 'team members' | 'GB dataset storage' | 'min API requests';
  key: keyof IFeatureProps;
}
const PricingCard: React.FC<IPricePlanWithButton> = (
  props: IPricePlanWithButton,
) => {
  const {
    title,
    description,
    price,
    feature,
    buttonLabel,
    isUse = false,
    icon,
  } = props;
  const suffix = [
    {
      id: 1,
      icon: <LayoutGrid size={12} className="text-gray-500 font-normal mr-2" />,
      text: 'Apps',
      key: 'apps',
    },
    {
      id: 2,
      icon: <Users size={12} className="text-gray-500 font-normal mr-2" />,
      text: 'team members',
      key: 'teamMembers',
    },
    {
      id: 3,
      icon: <Layers size={12} className="text-gray-500 font-normal mr-2" />,
      text: 'GB dataset storage',
      key: 'datasetStorage',
    },
    {
      id: 4,
      icon: (
        <GitPullRequestArrow
          size={12}
          className="text-gray-500 font-normal mr-2"
        />
      ),
      text: 'min API requests',
      key: 'apiRequests',
    },
  ] as ISuffixProps[];

  const handleBuy = () => {
    showPriceComfirmModal(props);
  };
  return (
    <div
      className={`price-modal-card rounded-lg shadow-lg p-6 text-center transition-transform hover:scale-105 bg-background-card`}
    >
      <div className="flex justify-between items-center mb-6">
        <div className="icon border-box  border-2 p-1 rounded-sm">{icon()}</div>
        <h3 className="text-3xl font-bold text-left">
          <span className="text-sm mr-1">$</span>
          {price}
          <span className="text-sm text-gray-500 font-normal ml-1">/month</span>
        </h3>
      </div>
      <h2 className="text-2xl font-bold mb-4 text-left">{title}</h2>
      <p className="mb-6 text-left h-16">{description}</p>
      <button
        type="button"
        className={classNames(
          'w-full py-1 rounded-md font-bold  text-black  hover:bg-sky-500 mb-6',
          { 'bg-gray-900': isUse, 'text-white': isUse, 'bg-white': !isUse },
        )}
        onClick={handleBuy}
      >
        {buttonLabel}
      </button>
      <ul className="mb-6">
        {suffix.map((item) => (
          <li key={item.id} className="mb-2 text-left">
            <div className="flex items-center">
              {item.icon}
              <span className="italic text-base font-semibold">
                {feature[item.key]}
              </span>
              <span className="ml-2 text-xm text-gray-500 font-normal">
                {item.text}
              </span>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
};

export default PricingCard;
