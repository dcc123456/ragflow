import { useTranslate } from '@/hooks/common-hooks';

import { useSystemConfig } from '@/hooks/use-system-request';
import { Navigate } from 'react-router';
import AuthCard from '../components/auth-card';
import BasicRegister from './basic';

export default function RegisterContainer() {
  const { t } = useTranslate('login');
  const { config } = useSystemConfig();
  const allowRegister = config?.registerEnabled !== 0;

  if (!allowRegister) {
    // Prevent user from accessing this page if registration is disabled
    return <Navigate to="/login" />;
  }

  return (
    <AuthCard title={t('signUpTitle')}>
      <BasicRegister />
    </AuthCard>
  );
}
