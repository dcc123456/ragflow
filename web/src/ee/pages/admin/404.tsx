import { useTranslation } from 'react-i18next';

export default function AdminNotFoundPage() {
  const { t } = useTranslation();

  return (
    <>
      <h1>{t('admin.registrationWhitelistNotEnabled')}</h1>
    </>
  );
}
