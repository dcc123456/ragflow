import { keyBy } from 'lodash';

import { zodResolver } from '@hookform/resolvers/zod';
import { t } from 'i18next';
import { useCallback, useId } from 'react';
import { useForm } from 'react-hook-form';
import { Link, useLocation, useNavigate } from 'react-router';
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

import SvgIcon from '@/components/svg-icon';
import { Button } from '@/components/ui/button';
import {
  useLogin,
  useLoginChannels,
  useLoginWithChannel,
} from '@/hooks/use-login-request';
import { useSystemConfig } from '@/hooks/use-system-request';
import { isLicenseError, rsaPsw } from '@/utils';
import RememberMeCheckbox from '../components/RememberMeCheckbox';

import { SSO_CLOUD_IDP_PROVIDERS } from '@/pages/admin/sso-providers/cloud-idp';

const SSO_PROVIDER_ICON_MAP = keyBy(SSO_CLOUD_IDP_PROVIDERS, 'key');

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

  const { t } = useTranslate('login');
  const { login, loading: loginLoading } = useLogin();
  const { config } = useSystemConfig();
  const { currentChannelType, typeGroupedChannels } = useLoginChannels();

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

  const allowRegister = config?.registerEnabled !== 0;
  const email = form.watch('email');

  const onSubmit = useCallback(
    async (data: SchemaType) => {
      try {
        const rsaPassword = rsaPsw(data.password) as string;
        const { code, message } = await login({
          email: data.email.trim(),
          password: rsaPassword,
        });

        if (code === 0) {
          navigate('/');
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
    [navigate, login, onLicenseError],
  );

  return (
    <Form {...form}>
      <form
        id={id}
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
            search: email ? `?u=${form.getValues('email')}` : '',
          }}
          state={form.getValues()}
          className="text-xs text-text-secondary hover:text-text-primary focus-visible:text-text-primary"
        >
          {t('forgetPassword')}
        </Link>
      </div>

      {currentChannelType === 'sso' && typeGroupedChannels.sso?.length ? (
        <div className="mt-8 flex justify-center items-center gap-2">
          {typeGroupedChannels.sso.map((ch) => {
            const IconComponent =
              SSO_PROVIDER_ICON_MAP[ch.channel as any]?.Icon;

            return (
              <Button
                key={ch.channel}
                className=""
                onClick={() => loginWithChannel(ch.channel)}
              >
                {IconComponent ? (
                  <IconComponent />
                ) : (
                  <SvgIcon name="sso" width={20} height={20} />
                )}

                {t('signInWith', { name: ch.display_name })}
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
