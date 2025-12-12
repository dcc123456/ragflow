import message from '@/components/ui/message';
import { useSetModalState } from '@/hooks/common-hooks';
import userService from '@/services/user-service';
import { rsaPsw } from '@/utils';
import { useCallback, useEffect, useState } from 'react';
export const useCountdown = (initialSeconds: number = 60) => {
  const [seconds, setSeconds] = useState(initialSeconds);
  const [isActive, setIsActive] = useState(false);

  const start = () => {
    if (seconds > 0) {
      setIsActive(true);
    }
  };

  const stop = () => {
    setIsActive(false);
  };

  const reset = useCallback(() => {
    setIsActive(false);
    setSeconds(initialSeconds);
  }, [initialSeconds]);

  useEffect(() => {
    let interval: NodeJS.Timeout | null = null;

    if (isActive && seconds > 0) {
      interval = setInterval(() => {
        setSeconds((prevSeconds) => prevSeconds - 1);
      }, 1000);
    } else if (seconds <= 0) {
      reset();
    }

    return () => {
      if (interval) clearInterval(interval);
    };
  }, [isActive, seconds, reset]);

  return { seconds, isActive, start, stop, reset };
};

export const useCaptcha = () => {
  const [captcha, setCaptcha] = useState('');
  const [imgLoading, setImgLoading] = useState(false);
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
    const res = await userService.loginGetCaptcha({
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
    setImgLoading(true);
    if (emailStr) {
      setEmail(emailStr);
    }
    const res = await fetchCaptcha(emailStr || email);
    setImgLoading(false);
    if (res instanceof Blob) {
      const url = URL.createObjectURL(res);
      setCaptcha(url);
      return true;
    } else {
      return false;
    }
    // }
  };

  const sendVerify = async (email: string, code: string) => {
    if (!code || !email) {
      return;
    }
    const res = await userService.loginSendVerifyCode({
      email,
      captcha: code,
    });
    if (res?.data.code === 0) {
      return true;
    } else {
      // message.error(res.data?.message);
      return false;
    }
    // return res?.data;
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
    imgLoading,
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

export const useVerifyCode = () => {
  const verifyEmail = async ({
    email,
    code,
  }: {
    email: string;
    code: string;
  }) => {
    const res = await userService.loginVerifyEmail({
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

export const usePasswordRequest = () => {
  const submit = async ({
    email,
    password,
    confirmPassword,
  }: {
    email: string;
    code: string;
    password: string;
    confirmPassword: string;
  }) => {
    const res = await userService.submitResetPassword({
      email,
      new_password: rsaPsw(password),
      confirm_new_password: rsaPsw(confirmPassword),
    });
    if (res?.data.code === 0) {
      return true;
    } else {
      return false;
    }
  };
  return { submit };
};
