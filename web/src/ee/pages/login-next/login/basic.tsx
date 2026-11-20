import { keyBy } from 'lodash';

import { zodResolver } from '@hookform/resolvers/zod';
import { t } from 'i18next';
import { useCallback, useEffect, useId, useState } from 'react';
import { useForm } from 'react-hook-form';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router';
import z from 'zod';

import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { useTranslate } from '@/hooks/common-hooks';

import { TruncatedText } from '@/components/truncated-text';
import { Button } from '@/components/ui/button';
import {
  useChannelLoginInitiate,
  useLogin,
  useLoginChannels,
  useLoginWithChannel,
} from '@/ee/hooks/use-login-request';
import type { ChannelLoginInitiation } from '@/ee/services/user-service';
import { useSystemConfig } from '@/hooks/use-system-request';
import { isLicenseError, rsaPsw } from '@/ee/utils';
import RememberMeCheckbox from '@/ee/pages/login-next/components/RememberMeCheckbox';
import FeishuQrLogin from '@/ee/pages/login-next/login/feishu-qr-login';
import WecomQrLogin from '@/ee/pages/login-next/login/wecom-qr-login';
import DingtalkQrLogin from '@/ee/pages/login-next/login/dingtalk-qr-login';

import { useAuth } from '@/hooks/auth-hooks';
import { SSO_CLOUD_IDP_PROVIDERS } from '@/ee/pages/admin/sso-providers/cloud-idp';

const SSO_PROVIDER_ICON_MAP: Partial<
  Record<string, (typeof SSO_CLOUD_IDP_PROVIDERS)[number]>
> = keyBy(SSO_CLOUD_IDP_PROVIDERS, 'key');

const schema = z.object({
  email: z
    .string()
    .email()
    .min(1, { message: t('login.emailPlaceholder') }),
  password: z.string().min(1, { message: t('login.passwordPlaceholder') }),
  remember: z.boolean().optional(),
});

type SchemaType = z.infer<typeof schema>;

type BasicLoginProps = {
  onLicenseError?: (error: boolean) => void;
};

export default function BasicLogin({ onLicenseError }: BasicLoginProps) {
  const id = useId();
  const location = useLocation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { t } = useTranslate('login');
  const { login, loading: loginLoading } = useLogin();
  const { config } = useSystemConfig();
  const { currentChannelType, typeGroupedChannels } = useLoginChannels();
  const { initiate: initiateChannel } = useChannelLoginInitiate();
  // Non-null while an embedded QR code (Feishu, WeCom) replaces the card;
  // the form and SSO icon row are replaced by the code until the user backs
  // out.
  const [embeddedQr, setEmbeddedQr] = useState<{
    channel: string;
    initiation: ChannelLoginInitiation;
  } | null>(null);
  const redirectTo =
    searchParams.get('redirect') ||
    (location.state as { redirectTo?: string } | null)?.redirectTo ||
    '/';
  const { login: loginWithChannel } = useLoginWithChannel();

  const form = useForm<SchemaType>({
    resolver: zodResolver(schema),
    defaultValues: {
      email: '',
      password: '',
      remember: false,
      ...(location.state || {}),
    },
  });
  const { isLogin } = useAuth();
  useEffect(() => {
    if (isLogin) {
      navigate(redirectTo, { replace: true });
    }
  }, [isLogin, navigate, redirectTo]);

  const allowRegister = config?.registerEnabled !== 0;
  const email = form.watch('email');

  // The Feishu, WeCom and DingTalk channels embed the official QR SDK on the
  // card itself instead of leaving the page: initiate fetches the authorize
  // parameters (the state cookie is written server-side), the widget renders
  // the code from them. Any failure — config incomplete, SDK unreachable —
  // falls back to the original full-page redirect flow.
  const handleChannelClick = useCallback(
    async (channel: string) => {
      if (channel !== 'lark' && channel !== 'wecom' && channel !== 'dingtalk') {
        loginWithChannel(channel);
        return;
      }

      try {
        const initiation = await initiateChannel(channel);
        if (initiation?.client_id && initiation.auth_url) {
          setEmbeddedQr({ channel, initiation });
          return;
        }
      } catch (e) {
        console.error(`${channel} QR login initiate failed:`, e);
      }
      loginWithChannel(channel);
    },
    [initiateChannel, loginWithChannel],
  );

  const onSubmit = useCallback(
    async (data: SchemaType) => {
      try {
        const rsaPassword = rsaPsw(data.password) as string;
        const { code, message } = await login({
          email: data.email.trim(),
          password: rsaPassword,
        });

        if (code === 0) {
          // navigate('/');
          navigate(redirectTo, { replace: true });
        } else if (isLicenseError(code, message)) {
          onLicenseError?.(true);
        } else {
          onLicenseError?.(false);
        }
      } catch (e) {
        onLicenseError?.(false);
        console.error('Login failed:', e);
      }
    },
    [navigate, login, onLicenseError, redirectTo],
  );

  if (embeddedQr) {
    if (embeddedQr.channel === 'wecom') {
      return (
        <WecomQrLogin
          authUrl={embeddedQr.initiation.auth_url}
          onBack={() => setEmbeddedQr(null)}
        />
      );
    }
    if (embeddedQr.channel === 'dingtalk') {
      return (
        <DingtalkQrLogin
          initiation={embeddedQr.initiation}
          onBack={() => setEmbeddedQr(null)}
        />
      );
    }
    return (
      <FeishuQrLogin
        authUrl={embeddedQr.initiation.auth_url}
        onBack={() => setEmbeddedQr(null)}
      />
    );
  }

  return (
    <Form {...form}>
      <form
        id={id}
        data-testid="auth-form"
        className="space-y-8"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <FormField
          control={form.control}
          name="email"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('emailLabel')}</FormLabel>
              <FormControl>
                <Input
                  data-testid="auth-email"
                  className="h-10"
                  placeholder={t('emailPlaceholder')}
                  autoComplete="email"
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="password"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('passwordLabel')}</FormLabel>
              <FormControl>
                <div className="relative">
                  <Input
                    data-testid="auth-password"
                    type="password"
                    className="h-10"
                    placeholder={t('passwordPlaceholder')}
                    autoComplete="current-password"
                    {...field}
                  />
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="remember"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                <FormControl>
                  <RememberMeCheckbox
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormLabel>
              <FormMessage />
            </FormItem>
          )}
        />

        <Button
          data-testid="auth-submit"
          type="submit"
          variant="metallic"
          loading={loginLoading}
          block
          className="!mt-12 h-10"
        >
          {t('login')}
        </Button>
      </form>

      <div className="mt-2 text-right">
        <Link
          to={{
            pathname: '/forget-password',
            search: email
              ? `?u=${encodeURIComponent(form.getValues('email'))}`
              : '',
          }}
          state={form.getValues()}
          className="text-xs text-text-secondary hover:text-text-primary focus-visible:text-text-primary"
        >
          {t('forgetPassword')}
        </Link>
      </div>

      {currentChannelType === 'sso' && typeGroupedChannels.sso?.length ? (
        <div className="mt-8 flex flex-wrap items-center justify-center gap-x-5 gap-y-2">
          {typeGroupedChannels.sso.map((channel) => {
            const IconComponent = SSO_PROVIDER_ICON_MAP[channel.channel]?.Icon;
            const name = channel.display_name || channel.channel;
            const handleClick = () => handleChannelClick(channel.channel);

            return (
              <Button
                key={channel.channel}
                type="button"
                variant={IconComponent ? 'icon' : 'outline'}
                size={IconComponent ? 'icon-xs' : 'default'}
                className={
                  IconComponent
                    ? 'border-0 focus-visible:ring-2 focus-visible:ring-accent-primary'
                    : 'h-10 w-full min-w-0'
                }
                aria-label={name}
                onClick={handleClick}
              >
                {IconComponent ? (
                  <IconComponent className="size-6" />
                ) : (
                  <TruncatedText
                    as="span"
                    className="min-w-0 truncate"
                    tooltip={name}
                  >
                    {name}
                  </TruncatedText>
                )}
              </Button>
            );
          })}
        </div>
      ) : null}

      {allowRegister && (
        <div className="mt-10 text-right">
          <p className="text-sm text-text-disabled">
            {t('signInTip')}{' '}
            <Link
              data-testid="auth-toggle-register"
              to="/register"
              state={form.getValues()}
              className="text-accent-primary/90 hover:text-accent-primary hover:bg-transparent font-medium"
            >
              {t('signUp')}
            </Link>
          </p>
        </div>
      )}
    </Form>
  );
}
