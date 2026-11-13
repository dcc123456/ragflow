import { Button, buttonVariants } from '@/components/ui/button';
import message from '@/components/ui/message';
import { Authorization } from '@/constants/authorization';
import {
  ITenantInviteLinkInfo,
  ITenantInviteLinkMembership,
} from '@/ee/interfaces/database/team';
import {
  getTenantInviteLinkMembership,
  getTenantInviteLinkInfo,
  requestTenantInviteLink,
} from '@/ee/services/tenant-invite-service';
import { switchTenant } from '@/ee/services/tenant-service';
import { useAuth } from '@/hooks/auth-hooks';
import authorizationUtil from '@/utils/authorization-util';
import { formatDate } from '@/utils/date';
import { useMutation, useQuery } from '@tanstack/react-query';
import { AxiosHeaders } from 'axios';
import { Loader2, Users } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useLocation, useParams } from 'react-router';

const TenantMembershipRequestedStatus = '4';
const TenantMembershipJoinedStatus = '1';

const TenantInvitationKeys = {
  invitation: (token: string) => ['tenantInviteLinkInfo', token] as const,
  membership: (token: string) => ['tenantInviteLinkMembership', token] as const,
};

export default function TenantInvitationPage() {
  const { t } = useTranslation();
  const { token = '' } = useParams();
  const location = useLocation();
  const { isLogin } = useAuth();
  const [requestSubmitted, setRequestSubmitted] = useState(false);
  const [switching, setSwitching] = useState(false);
  const redirectPath = `${location.pathname}${location.search}`;
  const base = import.meta.env.VITE_BASE_URL || '/';
  const appBasePath = base.endsWith('/') ? base : `${base}/`;
  const {
    data: invitation,
    isError,
    isLoading,
  } = useQuery({
    queryKey: TenantInvitationKeys.invitation(token),
    enabled: !!token,
    retry: false,
    queryFn: async (): Promise<ITenantInviteLinkInfo | undefined> => {
      const { data } = await getTenantInviteLinkInfo(token);

      return data?.code === 0 ? data.data : undefined;
    },
  });
  const {
    data: membershipStatus,
    isError: isMembershipError,
    isLoading: isMembershipLoading,
  } = useQuery<ITenantInviteLinkMembership>({
    queryKey: TenantInvitationKeys.membership(token),
    enabled: isLogin === true && !!invitation?.tenant_id,
    retry: false,
    queryFn: async () => {
      const { data } = await getTenantInviteLinkMembership(token);

      if (data?.code !== 0 || !data?.data) {
        throw new Error('Failed to load tenant memberships');
      }

      return data.data;
    },
  });
  const { isPending: requesting, mutateAsync: submitJoinRequest } = useMutation(
    {
      mutationFn: async () => {
        const { data } = await requestTenantInviteLink(token);

        return data?.code;
      },
    },
  );

  const handleRequestToJoin = async () => {
    const code = await submitJoinRequest();

    if (code === 0) {
      setRequestSubmitted(true);
    }
  };

  const handleOpenTenant = async () => {
    if (!invitation) {
      return;
    }

    setSwitching(true);
    try {
      const response = await switchTenant(invitation.tenant_id);
      const authorization = (response.headers as AxiosHeaders).get(
        Authorization,
      );

      if (response.data?.code === 0 && authorization) {
        authorizationUtil.setAuthorization(String(authorization));
        window.location.assign(appBasePath);
      } else {
        message.error(t('permission.tenantInvitationSwitchFailed'));
      }
    } catch {
      message.error(t('permission.tenantInvitationSwitchFailed'));
    } finally {
      setSwitching(false);
    }
  };

  const isAlreadyMember =
    membershipStatus?.status === TenantMembershipJoinedStatus;
  const isPendingReview =
    requestSubmitted ||
    membershipStatus?.status === TenantMembershipRequestedStatus;
  const loginUrl = `/login?redirect=${encodeURIComponent(redirectPath)}`;

  return (
    <main className="flex min-h-screen items-center justify-center bg-bg-base px-4 py-10">
      <section className="w-full max-w-lg space-y-6 rounded-xl border border-border-button bg-bg-card p-6 shadow-lg sm:p-8">
        <header className="space-y-3 text-center">
          <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-accent-primary/10 text-accent-primary">
            <Users className="size-6" />
          </div>
          <h1 className="text-xl font-semibold text-text-primary">
            {isAlreadyMember
              ? t('permission.tenantInvitationAlreadyJoinedTitle')
              : isPendingReview
                ? t('permission.tenantInvitationPendingReviewTitle')
                : t('permission.tenantInvitationTitle')}
          </h1>
        </header>

        {isLoading ||
        isLogin === null ||
        (isLogin && !!invitation && isMembershipLoading) ? (
          <div className="flex justify-center py-6">
            <Loader2 className="size-6 animate-spin text-text-secondary" />
          </div>
        ) : isError || !invitation ? (
          <p className="rounded-lg bg-state-error/10 p-4 text-center text-sm text-state-error">
            {t('permission.tenantInvitationInvalid')}
          </p>
        ) : isMembershipError ? (
          <p className="rounded-lg bg-state-error/10 p-4 text-center text-sm text-state-error">
            {t('permission.tenantInvitationMembershipLoadFailed')}
          </p>
        ) : isAlreadyMember ? (
          <div className="space-y-4 text-center">
            <p className="text-lg font-medium text-text-primary">
              {invitation.tenant_name}
            </p>
            <p className="rounded-lg bg-accent-primary/10 p-4 text-sm text-text-primary">
              {t('permission.tenantInvitationAlreadyJoined', {
                tenant: invitation.tenant_name,
              })}
            </p>
            <Button
              className="w-full"
              loading={switching}
              onClick={handleOpenTenant}
            >
              {t('permission.tenantInvitationOpenTenant')}
            </Button>
          </div>
        ) : isPendingReview ? (
          <div className="space-y-3 text-center">
            <p className="text-lg font-medium text-text-primary">
              {invitation.tenant_name}
            </p>
            <p className="rounded-lg bg-bg-base p-4 text-sm text-text-secondary">
              {t(
                requestSubmitted
                  ? 'permission.tenantInvitationRequestSubmitted'
                  : 'permission.tenantInvitationPendingReview',
                { tenant: invitation.tenant_name },
              )}
            </p>
          </div>
        ) : (
          <div className="space-y-5">
            <div className="space-y-2 text-center">
              <p className="break-words text-lg font-medium text-text-primary">
                {invitation.tenant_name}
              </p>
              <p className="break-words text-sm text-text-secondary">
                {t('permission.tenantInvitationInvitedBy', {
                  inviter: invitation.inviter,
                })}
              </p>
              <p className="text-xs text-text-disabled">
                {t('permission.inviteLinkExpiresAt', {
                  date: formatDate(invitation.expires_at),
                })}
              </p>
            </div>

            {isLogin ? (
              <Button
                className="w-full"
                loading={requesting}
                onClick={handleRequestToJoin}
              >
                {t('permission.tenantInvitationRequestJoin')}
              </Button>
            ) : (
              <div className="flex gap-3">
                <Link
                  to={loginUrl}
                  className={buttonVariants({
                    className:
                      'h-auto min-h-8 min-w-0 flex-1 whitespace-normal py-2 text-center leading-tight',
                  })}
                >
                  {t('permission.tenantInvitationSignIn')}
                </Link>
                <Link
                  to="/register"
                  state={{ redirectTo: redirectPath }}
                  className={buttonVariants({
                    variant: 'outline',
                    className:
                      'h-auto min-h-8 min-w-0 flex-1 whitespace-normal py-2 text-center leading-tight',
                  })}
                >
                  {t('permission.tenantInvitationRegister')}
                </Link>
              </div>
            )}
          </div>
        )}
      </section>
    </main>
  );
}
