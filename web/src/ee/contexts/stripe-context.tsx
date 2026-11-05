import { loadStripe, Stripe } from '@stripe/stripe-js';
import React, { createContext, useContext, useEffect, useState } from 'react';

interface StripeContextType {
  stripe: Stripe | null;
  isLoading: boolean;
  stripePublishableKey: string | null;
}

const StripeContext = createContext<StripeContextType>({
  stripe: null,
  isLoading: true,
  stripePublishableKey: null,
});

export const useStripe = () => useContext(StripeContext);

export const StripeProvider: React.FC<{
  children: React.ReactNode;
  publishableKey?: string | null;
}> = ({ children, publishableKey: initialPublishableKey = null }) => {
  const [stripe, setStripe] = useState<Stripe | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const initStripe = async () => {
      try {
        const key = initialPublishableKey;

        if (!key) {
          console.warn('Stripe publishable key not configured on server.');
          setIsLoading(false);
          return;
        }

        // Security check: only load if it starts with 'pk_'
        if (!key.startsWith('pk_')) {
          console.error('Invalid publishable key format - must start with pk_');
          setIsLoading(false);
          return;
        }

        const stripeInstance = await loadStripe(key);
        setStripe(stripeInstance);
      } catch (error) {
        console.error('Failed to load Stripe:', error);
      } finally {
        setIsLoading(false);
      }
    };

    initStripe();
  }, [initialPublishableKey]);

  return (
    <StripeContext.Provider
      value={{
        stripe,
        isLoading,
        stripePublishableKey: initialPublishableKey,
      }}
    >
      {children}
    </StripeContext.Provider>
  );
};
