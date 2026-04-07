import { useLocation, useNavigate, useSearchParams } from 'react-router';

import {
  DynamicForm,
  DynamicFormRef,
  FormFieldConfig,
} from '@/components/dynamic-form';
import Input from '@/components/originui/input';
import { Button } from '@/components/ui/button';
import message from '@/components/ui/message';
import { Modal } from '@/components/ui/modal/modal';
import { t } from 'i18next';
import { debounce } from 'lodash';
import { ArrowLeft } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import AuthCard from '../components/auth-card';
import { formFields } from './constant';
import {
  useCaptcha,
  useCountdown,
  usePasswordRequest,
  useVerifyCode,
} from './hook';

const stepMap = {
  1: {
    name: ['email'],
    btnName: t('login.send'),
  },
  2: {
    name: ['verifyCode'],
    btnName: t('login.verifyCode'),
  },
  3: { name: ['password', 'confirmPassword'], btnName: t('common.submit') },
};

export const ForgetPassword = ({
  backLogin,
}: {
  backLogin: ({
    email,
    password,
  }: {
    email?: string;
    password?: string;
  }) => void;
}) => {
  const [sp] = useSearchParams();
  const formEmail = sp.get('u') || '';

  const [fields, setFields] = useState<FormFieldConfig[]>(formFields);
  const formRef = useRef<DynamicFormRef>(null);
  const [step, setStep] = useState(1);
  const [email, setEmail] = useState(formEmail || '');
  const [verifyEmailCode, setVerifyEmailCode] = useState('');
  const { seconds, isActive, start } = useCountdown();
  const [defaultFormValues, setDefaultFormValues] = useState({
    email: formEmail || '',
  });
  const [sendLoading, setSendLoading] = useState(false);
  useEffect(() => {
    if (formEmail && step === 1) {
      setDefaultFormValues({
        email: formEmail,
      });
    }
  }, [formEmail, step]);

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
  } = useCaptcha();

  const { verifyEmail } = useVerifyCode();
  const { submit } = usePasswordRequest();

  useEffect(() => {
    const tempFields = formFields
      .filter((field) =>
        stepMap[step as keyof typeof stepMap].name.includes(field.name),
      )
      ?.map((field) => {
        if (field.name === 'email') {
          return {
            ...field,
            onChange: debounce(() => formRef.current?.trigger('email'), 500),
          };
        }
        return {
          ...field,
        };
      });
    setFields(tempFields);
  }, [step]);

  const handleSubmit = useCallback(
    async (num: -1 | 1) => {
      let pass = undefined;

      if (num > 0) {
        if (step > 3) {
          return;
        }
        if (step === 1) {
          setSendLoading(true);
          pass = await formRef.current?.trigger('email');
          if (pass) {
            const email = formRef.current?.getValues('email');
            setEmail(email);
            getCaptcha(email)
              .then((invalid: boolean) => {
                if (invalid) showEmbedModal();
              })
              .finally(() => {
                setSendLoading(false);
              });
          }
        }
        if (step === 2) {
          pass = await formRef.current?.trigger();
          const verifyEmailCode =
            await formRef.current?.getValues('verifyCode');
          setVerifyEmailCode(verifyEmailCode);
          if (pass) {
            const res = await verifyEmail({
              email,
              code: verifyEmailCode,
            });
            if (res) {
              setStep(step + num);
              message.success(t('message.success'));
              return;
            }
          }
        }
        if (step === 3) {
          pass = await formRef.current?.trigger();
          console.log(pass);
          if (pass) {
            const password = formRef.current?.getValues('password');
            const res = await submit({
              email,
              code: verifyEmailCode,
              password,
              confirmPassword: formRef.current?.getValues('confirmPassword'),
            });
            if (res) {
              // setStep(step + num);
              message.success(t('message.success'));
              backLogin?.({ email, password });
            }
            return;
          }
        }
      } else {
        if (step < 1) {
          return;
        }
        if (step === 1) {
          backLogin?.({});
        } else {
          setStep(step + num);
        }
      }
      console.log(pass, step);
      if (!pass) {
        return;
      }

      // setStep(step + num);
    },
    [
      step,
      getCaptcha,
      showEmbedModal,
      backLogin,
      email,
      verifyEmail,
      submit,
      verifyEmailCode,
    ],
  );

  return (
    <AuthCard
      className="mt-24"
      title={step === 3 ? t('login.resetPassword') : t('login.checkEmail')}
    >
      <div className="flex flex-col space-y-10">
        {(step === 1 || step === 2) && (
          <div
            className="text-xl"
            dangerouslySetInnerHTML={{
              __html:
                step === 1
                  ? t('login.sendTip')
                  : step === 2
                    ? t('login.verifyCodeTip', { email: email })
                    : '',
            }}
          ></div>
        )}

        <DynamicForm.Root
          ref={formRef}
          fields={fields}
          defaultValues={defaultFormValues}
          onSubmit={() => {}}
          labelClassName="text-base"
        ></DynamicForm.Root>
        <div className="w-full">
          <Button
            className="w-full p-5 text-base"
            variant={'default'}
            onClick={() => {
              handleSubmit(1);
            }}
            loading={step === 1 && sendLoading}
          >
            {stepMap[step as keyof typeof stepMap]?.btnName}
          </Button>
        </div>
      </div>
      <div className="flex w-full justify-between items-center">
        {step > 0 && (
          <Button
            className="text-sm text-text-secondary bg-transparent px-0 hover:bg-transparent"
            variant={'ghost'}
            onClick={() => {
              handleSubmit(-1);
            }}
          >
            <div className="flex items-center gap-2">
              <ArrowLeft />
              {t('login.back')}
            </div>
          </Button>
        )}
        {step === 2 && (
          <div className="text-sm text-text-secondary flex items-center gap-2">
            {t('login.notGotEmail')}
            {isActive && (
              <span className="text-sm text-accent-primary">{seconds}s</span>
            )}
            {!isActive && (
              <span
                className="text-sm text-accent-primary cursor-pointer"
                onClick={() => {
                  getCaptcha().then((invalid: boolean) => {
                    if (invalid) showEmbedModal();
                  });
                }}
              >
                {t('login.resendEmail')}
              </span>
            )}
          </div>
        )}
      </div>

      {embedVisible && (
        <Modal
          open={embedVisible}
          // onOpenChange={handleClose}
          className="!w-[480px]"
          maskClosable={false}
          onOk={() =>
            handleSendVerify({
              callback: () => {
                setStep(2);
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
                  alt={'captcha'}
                  className=" h-full"
                  onClick={() => getCaptcha()}
                />
              </div>
            )}
            <div className="w-full">
              <Input
                value={captchaValue}
                onChange={(e) => setCaptchaValue(e.target.value)}
                placeholder={t('login.captchaPlaceholder')}
              />
            </div>
          </div>
        </Modal>
      )}
    </AuthCard>
  );
};

export default function ForgetPasswordContainer() {
  const location = useLocation();
  const navigate = useNavigate();

  return (
    <ForgetPassword
      backLogin={(state) => {
        const returnData =
          state?.email && state?.password ? state : location.state;

        navigate('/login', { state: returnData });
      }}
    />
  );
}
