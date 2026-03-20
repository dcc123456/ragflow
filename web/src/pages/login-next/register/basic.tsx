import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { zodResolver } from '@hookform/resolvers/zod';
import { t } from 'i18next';
import { useCallback, useId } from 'react';
import { useForm } from 'react-hook-form';
import { Link, useLocation, useNavigate } from 'react-router';
import { z } from 'zod';

import { Button } from '@/components/ui/button';
import { useTranslate } from '@/hooks/common-hooks';
import { useRegister } from '@/hooks/use-login-request';
import { rsaPsw } from '@/utils';

const schema = z.object({
  nickname: z.string().min(1, { message: t('login.nicknamePlaceholder') }),
  email: z
    .string()
    .email()
    .min(1, { message: t('login.emailPlaceholder') }),
  password: z.string().min(1, { message: t('login.passwordPlaceholder') }),
});

type SchemaType = z.infer<typeof schema>;

export default function BasicRegister() {
  const id = useId();
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useTranslate('login');
  const { register, loading: registerLoading } = useRegister();

  const form = useForm<SchemaType>({
    resolver: zodResolver(schema),
    defaultValues: {
      nickname: '',
      email: '',
      password: '',
    },
  });

  const onSubmit = useCallback(
    async (data: SchemaType) => {
      try {
        const rsaPassword = rsaPsw(data.password) as string;
        const code = await register({
          email: data.email.trim(),
          password: rsaPassword,
          nickname: data.nickname,
        });
        if (code === 0) {
          navigate('/login');
        }
      } catch (error) {
        console.error('Register failed:', error);
      }
    },
    [navigate, register],
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
          name="nickname"
          render={({ field }) => (
            <FormItem>
              <FormLabel required>{t('nicknameLabel')}</FormLabel>
              <FormControl>
                <Input
                  className="h-10"
                  placeholder={t('nicknamePlaceholder')}
                  autoComplete="username"
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
                    className="h-10"
                    type="password"
                    placeholder={t('passwordPlaceholder')}
                    autoComplete="new-password"
                    {...field}
                  />
                </div>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <Button
          type="submit"
          variant="metallic"
          loading={registerLoading}
          block
          className="!mt-12 h-10"
        >
          {t('continue')}
        </Button>
      </form>

      <div className="mt-10 text-right">
        <p className="text-sm text-text-disabled">
          {t('signUpTip')}{' '}
          <Link
            to="/login"
            state={location.state}
            className="text-accent-primary/90 hover:text-accent-primary hover:bg-transparent font-medium"
          >
            {t('login')}
          </Link>
        </p>
      </div>
    </Form>
  );
}
