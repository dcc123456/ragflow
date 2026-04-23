import { useUpgradeModal } from '../gobal';

const UpgradeButton = ({
  text,
  isModal = true,
  onCallBack,
}: {
  text?: string;
  isModal?: boolean;
  onCallBack?: () => void;
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
      className="whitespace-nowrap inline-flex items-center pl-2 py-1 pr-1 text-bg-base font-semibold rounded-md bg-text-primary border-b-2 border-accent-primary"
    >
      {text ?? 'Upgrade Now'}
      {/* <ArrowUpRight /> */}
    </a>
  );
};

export default UpgradeButton;
