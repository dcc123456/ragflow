import { useTranslate } from '@/hooks/common-hooks';
import { useLoginChannels } from '@/hooks/use-login-request';

import { Skeleton } from '@/components/ui/skeleton';
import AuthCard from '../components/AuthCard';
import BasicLogin from './basic';
import LdapLogin from './ldap';

function LoginSkeleton() {
  return (
    <div className="h-full space-y-8 text-bg-card">
      <div className="space-y-4">
        <Skeleton className="bg-current w-20 h-2" />
        <Skeleton className="bg-current w-full h-10" />
      </div>

      <div className="space-y-4">
        <Skeleton className="bg-current w-20 h-2" />
        <Skeleton className="bg-current w-full h-10" />
      </div>

      <div className="flex items-center gap-1.5">
        <Skeleton className="bg-current size-4" />
        <Skeleton className="bg-current w-28 h-4" />
      </div>

      <Skeleton className="mt-16 bg-current w-full h-10" />
      <Skeleton className="!mt-2 ml-auto bg-current w-20 h-3" />
      <Skeleton className="!mt-10 ml-auto bg-current w-56 h-3" />
    </div>
  );
}

export default function LoginContainer() {
  const { t } = useTranslate('login');
  const { currentChannelType, loading: channelsLoading = true } =
    useLoginChannels();

  return (
    <AuthCard title={t('loginTitle')}>
      {channelsLoading ? (
        <LoginSkeleton />
      ) : currentChannelType === 'ldap' ? (
        <LdapLogin />
      ) : (
        <BasicLogin />
      )}
    </AuthCard>
  );
}
