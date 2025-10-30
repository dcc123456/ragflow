const UpgradeButton = ({ text }: { text?: string }) => {
  return (
    <a
      href="/price"
      className="whitespace-nowrap inline-flex items-center pl-2 py-1 pr-1 text-bg-base font-semibold rounded-md bg-text-primary border-b-2 border-accent-primary"
    >
      {text ?? 'Upgrade Now'}
      {/* <ArrowUpRight /> */}
    </a>
  );
};

export default UpgradeButton;
