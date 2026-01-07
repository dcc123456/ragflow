import { useEffect, useId, useState } from 'react';
import { useNavigate } from 'react-router';

import { BgSvg } from './bg';

import LoginWithLDAPForm, { type LDAPLoginFormState } from './ldap-form';

import Spotlight from '@/components/spotlight';
import { ButtonLoading } from '@/components/ui/button';

import { useAuth } from '@/hooks/auth-hooks';
import { useTranslate } from '@/hooks/common-hooks';
import { useLogin } from '@/hooks/use-login-request';
import { rsaPsw } from '@/utils';

import './index.less';

const LoginWithLDAP = () => {
  const navigate = useNavigate();
  const { login, loading: signLoading } = useLogin();
  const { t } = useTranslate('login');

  const [isUserInteracting] = useState(true);

  const loading = signLoading;
  const { isLogin } = useAuth();
  useEffect(() => {
    if (isLogin) {
      navigate('/');
    }
  }, [isLogin, navigate]);

  const formId = useId();
  const onCheck = async (params: LDAPLoginFormState) => {
    try {
      const rsaPassWord = rsaPsw(params.password) as string;

      const code = await login({
        ...params,
        password: rsaPassWord,
      });
      if (code === 0) {
        navigate('/');
      }
    } catch (errorInfo) {
      console.log('Failed:', errorInfo);
    }
  };

  return (
    <>
      <Spotlight opcity={0.4} coverage={60} color={'rgb(128, 255, 248)'} />
      <Spotlight
        opcity={0.3}
        coverage={12}
        X={'10%'}
        Y={'-10%'}
        color={'rgb(128, 255, 248)'}
      />
      <Spotlight
        opcity={0.3}
        coverage={12}
        X={'90%'}
        Y={'-10%'}
        color={'rgb(128, 255, 248)'}
      />
      <div className=" h-[inherit] relative overflow-auto">
        <BgSvg isPaused={isUserInteracting} />

        <div className="absolute top-3 flex flex-col items-center mb-12 w-full text-text-primary">
          <div className="flex items-center mb-4 w-full pl-10 pt-10 ">
            <div className="w-12 h-12 p-2 rounded-lg flex items-center justify-center mr-3">
              <img
                src={'/logo.svg'}
                alt="logo"
                className="size-8 mr-[12] cursor-pointer"
              />
            </div>
            <div className="text-xl font-bold self-center">RAGFlow</div>
          </div>
          <h1 className="text-[36px] font-medium  text-center mb-2">
            {t('title')}
          </h1>
          {/* border border-accent-primary rounded-full */}
          {/* <div className="mt-4 px-6 py-1 text-sm font-medium text-cyan-600  hover:bg-cyan-50 transition-colors duration-200 border-glow relative overflow-hidden">
            {t('start')}
          </div> */}
        </div>
        <div className="relative z-10 flex flex-col items-center justify-center min-h-[1050px] px-4 sm:px-6 lg:px-8">
          {/* Logo and Header */}

          {/* Login Form */}
          <div className="flex flex-col items-center justify-center w-full">
            <div className="text-center mb-8">
              <h2 className="text-xl font-semibold text-text-primary">
                {t('loginTitle')}
              </h2>
            </div>
            <div className=" w-full max-w-[540px] bg-bg-component backdrop-blur-sm rounded-2xl shadow-xl py-14 px-10 border border-border-button ">
              <LoginWithLDAPForm id={formId} onSubmit={onCheck} />

              <ButtonLoading
                form={formId}
                type="submit"
                size="lg"
                loading={loading}
                className="font-medium bg-metallic-gradient border-b-[#00BEB4] border-b-2 hover:bg-metallic-gradient hover:border-b-[#02bcdd] w-full mt-8"
              >
                {t('login')}
              </ButtonLoading>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

export default LoginWithLDAP;
