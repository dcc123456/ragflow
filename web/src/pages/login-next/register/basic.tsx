import { Input as OriginInput } from '@/components/originui/input';
import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import message from '@/components/ui/message';
import { Modal } from '@/components/ui/modal/modal';
import { zodResolver } from '@hookform/resolvers/zod';
import { t } from 'i18next';
import { useCallback, useId, useState } from 'react';
import { useForm } from 'react-hook-form';
import { Link, useLocation, useNavigate } from 'react-router';
import { z } from 'zod';

import { useTranslate } from '@/hooks/common-hooks';
import { useRegister } from '@/hooks/use-login-request';
import { useSystemConfig } from '@/hooks/use-system-request';
import { rsaPsw } from '@/utils';
import {
  useCountdown,
  useRegisterCaptcha,
  useRegisterVerifyCode,
} from './hook';

const registerSchema = z.object({
  nickname: z.string().min(1, { message: t('login.nicknamePlaceholder') }),
  email: z
    .string()
    .email()
    .min(1, { message: t('login.emailPlaceholder') }),
  password: z.string().min(1, { message: t('login.passwordPlaceholder') }),
});

const verifyCodeSchema = z.object({
  verifyCode: z.string().min(1, { message: t('login.verifyCodePlaceholder') }),
});

type RegisterSchemaType = z.infer<typeof registerSchema>;
type VerifyCodeSchemaType = z.infer<typeof verifyCodeSchema>;

export default function BasicRegister() {
  const id = useId();
  const navigate = useNavigate();
  const location = useLocation();
  const { t: translate } = useTranslate('login');
  const { register: registerUser, loading: registerLoading } = useRegister();
  const { config } = useSystemConfig();

  const [showVerifyStep, setShowVerifyStep] = useState(false);
  const [formData, setFormData] = useState<RegisterSchemaType | null>(null);

  const { seconds, isActive, start } = useCountdown();
  const {
    captcha,
    handleClose,
    setCaptchaValue,
    captchaValue,
    embedVisible,
    showEmbedModal,
    handleSendVerify,
    verifyLoading,
    getCaptcha,
  } = useRegisterCaptcha();
  const { verifyEmail } = useRegisterVerifyCode();

  const registerForm = useForm<RegisterSchemaType>({
    resolver: zodResolver(registerSchema),
    defaultValues: {
      nickname: '',
      email: '',
      password: '',
    },
  });

  const verifyForm = useForm<VerifyCodeSchemaType>({
    resolver: zodResolver(verifyCodeSchema),
    defaultValues: {
      verifyCode: '',
    },
  });

  const onRegisterSubmit = useCallback(
    async (data: RegisterSchemaType) => {
      try {
        const trimmedEmail = data.email.trim();
        setFormData(data);
        if (config?.emailVerificationEnabled) {
          getCaptcha(trimmedEmail).then((valid: boolean) => {
            if (valid) showEmbedModal();
          });
        } else {
          // Skip email verification, register directly
          try {
            const rsaPassword = rsaPsw(data.password) as string;
            const code = await registerUser({
              email: trimmedEmail,
              password: rsaPassword,
              nickname: data.nickname,
            });
            if (code === 0) {
              navigate('/login');
            }
          } catch (error) {
            console.error('Register failed:', error);
          }
        }
      } catch (error) {
        console.error('Get captcha failed:', error);
      }
    },
    [
      getCaptcha,
      showEmbedModal,
      config?.emailVerificationEnabled,
      registerUser,
      navigate,
    ],
  );

  const onVerifySubmit = useCallback(
    async (data: VerifyCodeSchemaType) => {
      if (!formData) return;
      const res = await verifyEmail({
        email: formData.email.trim(),
        code: data.verifyCode,
      });
      if (res) {
        message.success(t('message.success'));
        // After email verification, submit the registration
        try {
          const rsaPassword = rsaPsw(formData.password) as string;
          const code = await registerUser({
            email: formData.email.trim(),
            password: rsaPassword,
            nickname: formData.nickname,
          });
          if (code === 0) {
            navigate('/login');
          }
        } catch (error) {
          console.error('Register failed:', error);
        }
      }
    },
    [formData, verifyEmail, registerUser, navigate],
  );

  const registerFormContent = () => {
    if (showVerifyStep && formData) {
      return (
        <div className="space-y-8">
          <div
            className="text-xl"
            dangerouslySetInnerHTML={{
              __html: t('login.registerVerifyCodeTip', {
                email: formData.email.trim(),
              }),
            }}
          />
          <Form {...verifyForm}>
            <form
              id={id}
              data-testid="auth-form-verify"
              className="space-y-8"
              onSubmit={verifyForm.handleSubmit(onVerifySubmit)}
            >
              <FormField
                control={verifyForm.control}
                name="verifyCode"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel required>
                      {translate('verificationCode')}
                    </FormLabel>
                    <FormControl>
                      <OriginInput
                        className="h-10"
                        placeholder={translate('verifyCodePlaceholder')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <Button
                data-testid="auth-submit-verify"
                type="submit"
                variant="metallic"
                loading={registerLoading}
                block
                className="!mt-12 h-10"
              >
                {translate('verifyCode')}
              </Button>
            </form>
          </Form>

          <div className="flex w-full items-center justify-between">
            <Button
              className="text-sm text-text-secondary bg-transparent px-0 hover:bg-transparent"
              variant="ghost"
              onClick={() => setShowVerifyStep(false)}
            >
              {translate('back')}
            </Button>
            <div className="text-sm text-text-secondary flex items-center gap-2">
              {translate('notGotEmail')}
              {isActive && (
                <span className="text-sm text-accent-primary">{seconds}s</span>
              )}
              {!isActive && (
                <span
                  className="text-sm text-accent-primary cursor-pointer"
                  onClick={() => {
                    getCaptcha().then((valid: boolean) => {
                      if (valid) showEmbedModal();
                    });
                  }}
                >
                  {translate('resendEmail')}
                </span>
              )}
            </div>
          </div>
        </div>
      );
    }
    return (
      <>
        <Form {...registerForm}>
          <form
            id={id}
            data-testid="auth-form"
            className="space-y-8"
            onSubmit={registerForm.handleSubmit(onRegisterSubmit)}
          >
            <FormField
              control={registerForm.control}
              name="email"
              render={({ field }) => (
                <FormItem>
                  <FormLabel required>{translate('emailLabel')}</FormLabel>
                  <FormControl>
                    <OriginInput
                      data-testid="auth-email"
                      className="h-10"
                      placeholder={translate('emailPlaceholder')}
                      autoComplete="email"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={registerForm.control}
              name="nickname"
              render={({ field }) => (
                <FormItem>
                  <FormLabel required>{translate('nicknameLabel')}</FormLabel>
                  <FormControl>
                    <OriginInput
                      data-testid="auth-nickname"
                      className="h-10"
                      placeholder={translate('nicknamePlaceholder')}
                      autoComplete="username"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={registerForm.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel required>{translate('passwordLabel')}</FormLabel>
                  <FormControl>
                    <div className="relative">
                      <OriginInput
                        data-testid="auth-password"
                        className="h-10"
                        type="password"
                        placeholder={translate('passwordPlaceholder')}
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
              data-testid="auth-submit"
              type="submit"
              variant="metallic"
              loading={!config?.emailVerificationEnabled && registerLoading}
              block
              className="!mt-12 h-10"
            >
              {translate('continue')}
            </Button>
          </form>
        </Form>

        <div className="mt-10 text-right">
          <p className="text-sm text-text-disabled">
            {translate('signUpTip')}{' '}
            <Link
              data-testid="auth-toggle-login"
              to="/login"
              state={location.state}
              className="text-accent-primary/90 hover:text-accent-primary hover:bg-transparent font-medium"
            >
              {translate('login')}
            </Link>
          </p>
        </div>
      </>
    );
  };

  return (
    <>
      {registerFormContent()}
      {embedVisible && (
        <Modal
          open={embedVisible}
          className="!w-[480px]"
          maskClosable={false}
          onOk={() =>
            handleSendVerify({
              callback: () => {
                setShowVerifyStep(true);
                start();
              },
            })
          }
          onCancel={handleClose}
          confirmLoading={verifyLoading}
        >
          <div className="flex flex-col space-y-5 items-center justify-center">
            {captcha && (
              <div className="h-10">
                <img
                  src={captcha}
                  alt="captcha"
                  className="h-full"
                  onClick={() => getCaptcha()}
                />
              </div>
            )}
            <div className="w-full">
              <OriginInput
                value={captchaValue}
                onChange={(e) => setCaptchaValue(e.target.value)}
                placeholder={translate('captchaPlaceholder')}
              />
            </div>
          </div>
        </Modal>
      )}
    </>
  );
}
