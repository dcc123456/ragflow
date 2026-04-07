import { useTranslate } from '@/hooks/common-hooks';
import { useLoginChannels } from '@/hooks/use-login-request';
import { AlertTriangle } from 'lucide-react';
import { useState } from 'react';

import { Skeleton } from '@/components/ui/skeleton';
import AuthCard from '../components/auth-card';
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

function LicenseErrorHeader() {
  const { t } = useTranslate('login');
  return (
    <div className="flex items-center justify-center gap-2 rounded-lg bg-state-error-5 px-4 py-3 text-state-error border border-border-button">
      <AlertTriangle className="size-3 shrink-0" />
      <p className="text-sm">{t('licenseExpired')}</p>
    </div>
  );
}
export default function LoginContainer() {
  const { t } = useTranslate('login');
  const { currentChannelType, loading: channelsLoading = true } =
    useLoginChannels();
  const [licenseError, setLicenseError] = useState(false);

  const header = licenseError ? <LicenseErrorHeader /> : undefined;

  return (
    <AuthCard title={t('loginTitle')} header={header}>
      {channelsLoading ? (
        <LoginSkeleton />
      ) : currentChannelType === 'ldap' ? (
        <LdapLogin onLicenseError={setLicenseError} />
      ) : (
        <BasicLogin onLicenseError={setLicenseError} />
      )}
    </AuthCard>
  );
}
