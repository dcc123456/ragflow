import { KeyRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export function PrivilegeDropdown() {
  const { t } = useTranslation();

  return (
    <div className="flex items-center justify-between w-full gap-2">
      {t('permission.permissionManagement')}
      <KeyRound className="size-3" />
    </div>
  );
}
