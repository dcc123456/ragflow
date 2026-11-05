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
