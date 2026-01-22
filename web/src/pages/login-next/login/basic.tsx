import { useCallback, useId } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';

import { zodResolver } from '@hookform/resolvers/zod';
import { t } from 'i18next';
import { useForm } from 'react-hook-form';
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

import { Button } from '@/components/ui/button';
import { useLogin } from '@/hooks/use-login-request';
import { useSystemConfig } from '@/hooks/use-system-request';
import { rsaPsw } from '@/utils';
import RememberMeCheckbox from '../components/RememberMeCheckbox';

const schema = z.object({
  email: z
    .string()
    .email()
    .min(1, { message: t('login.emailPlaceholder') }),
  password: z.string().min(1, { message: t('login.passwordPlaceholder') }),
  remember: z.boolean().optional(),
});

type SchemaType = z.infer<typeof schema>;

export default function BasicLogin() {
  const id = useId();
  const location = useLocation();
  const navigate = useNavigate();

  const { t } = useTranslate('login');
  const { login, loading: loginLoading } = useLogin();
  const { config } = useSystemConfig();

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
        const code = await login({
          email: data.email.trim(),
          password: rsaPassword,
        });

        if (code === 0) {
          navigate('/');
        }
      } catch (e) {
        console.error('Login failed:', e);
      }
    },
    [navigate, login],
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
      </form>

      <Button
        type="submit"
        variant="metallic"
        form={id}
        loading={loginLoading}
        block
        className="mt-16 h-10"
      >
        {t('login')}
      </Button>

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
