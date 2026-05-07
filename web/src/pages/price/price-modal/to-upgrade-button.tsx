import { cn } from '@/lib/utils';
import { ArrowUpRight } from 'lucide-react';
import { useUpgradeModal } from '../global';

const UpgradeButton = ({
  text,
  isModal = true,
  onCallBack,
  className,
}: {
  text?: string;
  isModal?: boolean;
  onCallBack?: () => void;
  className?: string;
}) => {
  const { openModal } = useUpgradeModal();

  const handleClick = (e: React.MouseEvent) => {
    if (isModal) {
      e.preventDefault();
      openModal();
      onCallBack?.();
    }
  };
  return (
    <a
      href="/price"
      onClick={handleClick}
      className={cn(
        'whitespace-nowrap inline-flex font-normal  items-center pl-2 py-1 pr-1 text-bg-base rounded-md bg-bg-input border-0.5 border-border-default text-text-secondary hover:text-text-primary hover:border-text-primary',
        className,
      )}
    >
      {text ?? 'Upgrade Now'}
      <ArrowUpRight />
    </a>
  );
};

export default UpgradeButton;
