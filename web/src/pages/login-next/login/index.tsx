import { useTranslate } from '@/hooks/common-hooks';

import AuthCard from '../components/AuthCard';
import BasicLogin from './basic';

export default function LoginContainer() {
  const { t } = useTranslate('login');

  return (
    <AuthCard title={t('loginTitle')}>
      <BasicLogin />
    </AuthCard>
  );
}
