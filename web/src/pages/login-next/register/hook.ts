import message from '@/components/ui/message';
import { useSetModalState } from '@/hooks/common-hooks';
import userService from '@/services/user-service';
import { useCallback, useState } from 'react';

export { useCountdown } from '../hooks/use-countdown';

export const useRegisterCaptcha = () => {
  const [captcha, setCaptcha] = useState('');
  const [captchaValue, setCaptchaValue] = useState('');
  const [verifyLoading, setVerifyLoading] = useState(false);
  const {
    visible: embedVisible,
    hideModal: hideEmbedModal,
    showModal: showEmbedModal,
  } = useSetModalState();
  const [email, setEmail] = useState('');

  const fetchCaptcha = async (email: string) => {
    if (!email) {
      return {};
    }
    const res = await userService.registerGetCaptcha({
      email,
    });

    if (res?.data && res.data.type === 'image/jpeg') {
      return res.data;
    } else {
      if (res.data instanceof Blob) {
        if (res.data.type === 'application/json') {
          try {
            const text = await res.data.text();
            const jsonData = JSON.parse(text);
            message.error(jsonData.message);
          } catch (error) {
            console.error('Error parsing blob to JSON:', error);
          }
        }
      }
      return false;
    }
  };

  const getCaptcha = async (emailStr?: string) => {
    if (emailStr) {
      setEmail(emailStr);
    }
    const res = await fetchCaptcha(emailStr || email);
    if (res instanceof Blob) {
      const url = URL.createObjectURL(res);
      setCaptcha(url);
      return true;
    } else {
      return false;
    }
  };

  const sendVerify = async (email: string, code: string) => {
    if (!code || !email) {
      return;
    }
    const res = await userService.registerSendVerifyCode({
      email,
      captcha: code,
    });
    if (res?.data.code === 0) {
      return true;
    } else {
      return false;
    }
  };

  const handleClose = useCallback(() => {
    hideEmbedModal();
    setCaptchaValue('');
    setCaptcha('');
  }, [hideEmbedModal]);

  const handleSendVerify = useCallback(
    async ({ callback }: { callback?: () => void }) => {
      setVerifyLoading(true);
      const res = await sendVerify(email, captchaValue);
      if (res) {
        handleClose();
        callback?.();
      }
      setVerifyLoading(false);
    },
    [email, captchaValue, handleClose],
  );

  return {
    captcha,
    setCaptchaValue,
    captchaValue,
    embedVisible,
    showEmbedModal,
    handleSendVerify,
    verifyLoading,
    getCaptcha,
    handleClose,
  };
};

export const useRegisterVerifyCode = () => {
  const verifyEmail = async ({
    email,
    code,
  }: {
    email: string;
    code: string;
  }) => {
    const res = await userService.registerVerifyEmail({
      email,
      otp: code,
    });
    if (res?.data.code === 0) {
      return true;
    } else {
      return false;
    }
  };
  return { verifyEmail };
};
