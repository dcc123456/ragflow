import { useState } from 'react';
import { PriceModalComponent } from '../price-modal/price-modal';
import { UpgradeTipsModal } from '../price-modal/upgrade-tips-modal';

export const PriceGobal = () => {
  const [openPriceModal, setOpenPriceModal] = useState(false);
  const [openUpgradeTipsModal, setOpenUpgradeTipsModal] = useState(false);
  const [upgradeTip, setUpgradeTip] = useState<{
    type: 'team-member' | 'dataset';
    message:
      | 'Your dataset is full (10 GB/10 GB). '
      | 'Team member limit reached (5/5)';
  }>({
    type: 'dataset',
    message: 'Your dataset is full (10 GB/10 GB). ',
  });
  // useEffect(() => {
  //   if (false) {
  //     showUpgradeTipsModal({
  //       type: 'team-member',
  //       message: 'Your dataset is full (10 GB/10 GB). ',
  //     });
  //   }
  // }, []);
  return (
    <>
      <PriceModalComponent
        isOpen={openPriceModal}
        onClose={() => {
          setOpenPriceModal(false);
        }}
      />
      <UpgradeTipsModal
        isOpen={openUpgradeTipsModal}
        type={upgradeTip.type}
        message={upgradeTip.message}
        onClose={() => {
          setOpenUpgradeTipsModal(false);
        }}
      />
    </>
  );
};
