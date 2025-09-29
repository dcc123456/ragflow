import { ArrowUpRight } from 'lucide-react';

const UpgradeButton = ({ text }: { text?: string }) => {
  return (
    <a
      href="/price"
      className="whitespace-nowrap inline-flex items-center pl-2 py-1 pr-1 text-white font-semibold rounded-md bg-gradient-to-r from-teal-300 to-blue-600 transition duration-300 ease-in-out hover:from-teal-400 hover:to-blue-700"
    >
      {text ?? 'Upgrade Now'}
      <ArrowUpRight />
    </a>
  );
};

export default UpgradeButton;
