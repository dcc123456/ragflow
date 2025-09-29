import { LabelMap } from '@/constants/team';
import { useTranslation } from 'react-i18next';

type PrivilegeLabelProps = {
  permission?: number;
};

export function PrivilegeLabel({ permission }: PrivilegeLabelProps) {
  const { t } = useTranslation();
  return typeof permission === 'number' && permission !== 0 ? (
    <span className="bg-gray-100 px-2 rounded text-blue-700">
      {t(`permission.${LabelMap[permission]}Permission`)}
    </span>
  ) : (
    <span></span>
  );
}
