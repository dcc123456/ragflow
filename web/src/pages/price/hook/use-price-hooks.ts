import message from '@/components/ui/message';
import { Modal } from '@/components/ui/modal/modal';
import { useFetchTenantData } from '@/hooks/use-user-setting-request';
import { BillingQueryKey } from '@/pages/billing/constants/query-keys';
import type { SessionData } from '@/pages/billing/hook/use-payment-status-request';
import { isBillingEnabled } from '@/services/billingStatus';
import billingService, {
  billingCheckout,
  postBillingStorageSetTarget,
} from '@/services/price';
import storagePrivate from '@/utils/authorization-private-util';
import storage from '@/utils/authorization-util';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import React, { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  ICheckoutResult,
  ICurrentPlan,
  IPlan,
  IPricePlanWithButton,
} from '../interface';
import { showModal } from '../price-modal/show-modal';
export type IChargePlan = {
  subscription_price_id?: string;
  quantity: string;
  usage_based_price_id?: string;
};
export const PriceChargeKey = 'price-charge';
export const TrialUpgradeSetupRetryKey = 'trial-upgrade-setup-retry';
export const StorageAddonSetupRetryKey = 'storage-addon-setup-retry';
export const TrialUpgradeSetupRetryResultKey =
  'trial-upgrade-setup-retry-result';
export const BillingDirectCheckoutResultEvent =
  'billing-direct-checkout-result';

type PlanSetupRetryPayload = {
  price_id: string;
  quantity: string;
  payment_type: 'subscription' | 'usage_based';
  auto_retry_pending?: boolean;
  auto_retry_started?: boolean;
};

type StorageSetupRetryPayload = {
  tenant_id?: string;
  target_storage_bytes: number;
  auto_retry_pending?: boolean;
  auto_retry_started?: boolean;
};

const buildCheckoutUrls = () => {
  const url = new URL(window.location.href);
  const successUrl = new URL(url.toString());
  const errorUrl = new URL(url.toString());

  successUrl.searchParams.set('price-pay-status', 'success');
  errorUrl.searchParams.set('price-pay-status', 'cancel');

  return {
    successUrl: successUrl.toString(),
    errorUrl: errorUrl.toString(),
  };
};

const formatCurrencyAmount = (amountCents?: number, currency?: string) => {
  if (amountCents === undefined) {
    return undefined;
  }

  const amount = amountCents / 100;
  const normalizedCurrency = (currency || 'USD').toUpperCase();

  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: normalizedCurrency,
    }).format(amount);
  } catch {
    return `${normalizedCurrency} ${amount.toLocaleString()}`;
  }
};

const publishDirectCheckoutResult = (res?: ICheckoutResult) => {
  if (!res) {
    return;
  }

  const payload = {
    status: 'paid',
    amount:
      typeof res.amount_cents === 'number' ? res.amount_cents / 100 : undefined,
    currency: res.currency,
    invoice_id: res.invoice_id,
    subscription_id: res.subscription_id,
    plan_name: res.plan_name,
    price_id: res.price_id,
  } satisfies SessionData;

  sessionStorage.setItem(
    TrialUpgradeSetupRetryResultKey,
    JSON.stringify(payload),
  );
  window.dispatchEvent(
    new CustomEvent(BillingDirectCheckoutResultEvent, {
      detail: payload,
    }),
  );
};

const openSetupCheckoutPreservingPage = (redirectTo?: string) => {
  if (!redirectTo) {
    return;
  }

  const paymentSetupWindow = window.open(redirectTo, '_blank');
  try {
    if (paymentSetupWindow) {
      paymentSetupWindow.opener = null;
    }
  } catch {
    // Ignore cross-origin opener hardening failures.
  }
  if (!paymentSetupWindow) {
    window.location.href = redirectTo;
  }
};

const openPaymentRedirectPreservingPage = (redirectTo?: string) => {
  if (!redirectTo) {
    return;
  }

  const paymentWindow = window.open(redirectTo, '_blank');
  try {
    if (paymentWindow) {
      paymentWindow.opener = null;
    }
  } catch {
    // Ignore cross-origin opener hardening failures.
  }
  if (paymentWindow) {
    window.close();
    return;
  }

  window.location.href = redirectTo;
};

export const useCancelPlan = () => {
  const queryClient = useQueryClient();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: ['cancelPlan'],
    mutationFn: async ({
      tenantId,
      targetPriceId,
    }: {
      tenantId: string;
      targetPriceId: string;
    }) => {
      const { successUrl, errorUrl } = buildCheckoutUrls();
      const { data: res } = await billingCheckout({
        tenant_id: tenantId,
        subscription_price_id: targetPriceId,
        payment_type: 'subscription',
        quantity: '1',
        session_cancel_url: errorUrl,
        session_success_url: successUrl,
      });
      return res;
    },
  });

  const cancel = async (tenantId: string, targetPriceId: string) => {
    const result = await mutateAsync({ tenantId, targetPriceId });
    if (result?.code === 0) {
      if (result.data?.redirect_to) {
        window.location.href = result.data.redirect_to;
      }
      message.success(result.message);
      await queryClient.invalidateQueries({
        queryKey: [BillingQueryKey.CurrentPlan],
      });
    }
    return result;
  };

  return { data, loading, cancel };
};
const useCharge = () => {
  const { data: tenantInfo } = useFetchTenantData();
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const tenantId = tenantInfo?.tenant_id;
  const cachedCurrentPlan = storagePrivate.getPricePlan() as
    | ICurrentPlan
    | undefined;
  const { successUrl, errorUrl } = buildCheckoutUrls();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: ['setDialog'],
    mutationFn: async ({
      price_id,
      quantity,
      payment_type,
    }: {
      price_id: string;
      quantity: string;
      payment_type: 'subscription' | 'usage_based';
    }) => {
      const resolvedTenantId = tenantId || cachedCurrentPlan?.tenant_id;

      if (!resolvedTenantId) {
        throw new Error('Unable to determine tenant for billing checkout');
      }

      if (!price_id) {
        throw new Error('Missing target price id for billing checkout');
      }

      const { data } = await billingCheckout({
        tenant_id: resolvedTenantId,
        subscription_price_id:
          payment_type === 'subscription' ? price_id : undefined,
        payment_type: payment_type,
        // quantity: chargePlan.quantity,
        quantity: quantity,
        // usage_based_price_id: chargePlan.usage_based_price_id,
        usage_based_price_id:
          payment_type === 'usage_based' ? price_id : undefined,
        // 'price_1RRTpfPtsKvwvC5fVsZly0mE',
        session_cancel_url: errorUrl,
        session_success_url: successUrl,
      });
      if (data.code === 0) {
        if (data.data?.requires_payment_method_setup) {
          localStorage.setItem(
            TrialUpgradeSetupRetryKey,
            JSON.stringify({
              price_id,
              quantity,
              payment_type,
              auto_retry_pending: true,
            }),
          );
        }
        return data.data as ICheckoutResult;
      }
      throw new Error(data?.message || 'Modify subscription failed');
    },
  });

  const charge = async (data: IPricePlanWithButton) => {
    if (data.isUse) {
      return;
    }

    let chargeResult;
    try {
      chargeResult = await mutateAsync({
        price_id: data.id,
        quantity: '1',
        payment_type: 'subscription',
      });
    } catch (error) {
      message.error((error as Error)?.message || 'Modify subscription failed');
      return;
    }
    if (chargeResult && chargeResult.redirect_to) {
      if (chargeResult.requires_payment_method_setup) {
        openSetupCheckoutPreservingPage(chargeResult.redirect_to);
      } else {
        window.location.href = chargeResult.redirect_to;
      }
    } else if (chargeResult && chargeResult.scheduled_change) {
      const effectiveAt = chargeResult?.scheduled_change?.effective_at;
      const modal = showModal({
        children: React.createElement(
          Modal,
          {
            open: true,
            title: 'Downgrade scheduled',
            onOpenChange: (open: boolean) => {
              if (!open) {
                modal.destroy();
              }
            },
            className: '!w-[400px]',
            footer: React.createElement(
              'div',
              { className: 'flex justify-end gap-2' },
              React.createElement(
                'button',
                {
                  type: 'button',
                  onClick: () => modal.destroy(),
                  className:
                    'px-2 py-1 bg-primary text-primary-foreground rounded-md hover:bg-primary/90',
                },
                'OK',
              ),
            ),
          },
          React.createElement(
            'div',
            { className: 'h-32' },
            `Your plan will downgrade at the end of the current billing period${effectiveAt ? ` (${effectiveAt})` : ''}.`,
          ),
        ),
      });
    } else if (chargeResult) {
      publishDirectCheckoutResult(chargeResult);
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.CurrentPlan],
        }),
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.PlanOverview],
        }),
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.BaseOverview],
        }),
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.StorageCurrent],
        }),
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.PlanList],
        }),
      ]);
      const amountText = formatCurrencyAmount(
        chargeResult.amount_cents,
        chargeResult.currency,
      );
      message.success(
        amountText
          ? `${t('price.paymentSuccessfulTip')} (${amountText})`
          : t('price.paymentSuccessfulTip'),
      );
    }
  };
  return { data, loading, charge, checkout: mutateAsync };
};

export const useHandleTrialUpgradeSetupRetry = (status: string | null) => {
  const { checkout } = useCharge();
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  useEffect(() => {
    if (status !== 'success') {
      return;
    }

    const invalidateBillingQueries = () =>
      Promise.all([
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.CurrentPlan],
        }),
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.PlanOverview],
        }),
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.BaseOverview],
        }),
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.StorageCurrent],
        }),
        queryClient.invalidateQueries({
          queryKey: [BillingQueryKey.PlanList],
        }),
      ]);

    const rawPlanRetryPayload = localStorage.getItem(TrialUpgradeSetupRetryKey);
    if (rawPlanRetryPayload) {
      let retryPayload: PlanSetupRetryPayload | null = null;
      try {
        retryPayload = JSON.parse(rawPlanRetryPayload) as PlanSetupRetryPayload;
      } catch {
        localStorage.removeItem(TrialUpgradeSetupRetryKey);
        return;
      }

      if (
        retryPayload?.auto_retry_pending &&
        !retryPayload?.auto_retry_started &&
        retryPayload?.price_id
      ) {
        localStorage.setItem(
          TrialUpgradeSetupRetryKey,
          JSON.stringify({
            ...retryPayload,
            auto_retry_started: true,
          }),
        );

        checkout({
          price_id: retryPayload.price_id,
          quantity: retryPayload.quantity || '1',
          payment_type: retryPayload.payment_type || 'subscription',
        })
          .then(async (res) => {
            localStorage.removeItem(TrialUpgradeSetupRetryKey);
            if (res?.redirect_to) {
              openPaymentRedirectPreservingPage(res.redirect_to);
              return;
            }
            publishDirectCheckoutResult(res);
            await invalidateBillingQueries();
            const amountText = formatCurrencyAmount(
              res?.amount_cents,
              res?.currency,
            );
            message.success(
              amountText
                ? `${t('price.paymentSuccessfulTip')} (${amountText})`
                : t('price.paymentSuccessfulTip'),
            );
          })
          .catch((error) => {
            localStorage.removeItem(TrialUpgradeSetupRetryKey);
            message.error(
              (error as Error)?.message || t('price.paymentFailedTip'),
            );
          });
        return;
      }
    }

    const rawStorageRetryPayload = localStorage.getItem(
      StorageAddonSetupRetryKey,
    );
    if (!rawStorageRetryPayload) {
      return;
    }

    let storageRetryPayload: StorageSetupRetryPayload | null = null;
    try {
      storageRetryPayload = JSON.parse(
        rawStorageRetryPayload,
      ) as StorageSetupRetryPayload;
    } catch {
      localStorage.removeItem(StorageAddonSetupRetryKey);
      return;
    }

    if (
      !storageRetryPayload?.auto_retry_pending ||
      storageRetryPayload?.auto_retry_started ||
      typeof storageRetryPayload?.target_storage_bytes !== 'number'
    ) {
      return;
    }

    localStorage.setItem(
      StorageAddonSetupRetryKey,
      JSON.stringify({
        ...storageRetryPayload,
        auto_retry_started: true,
      }),
    );

    const { successUrl, errorUrl } = buildCheckoutUrls();
    postBillingStorageSetTarget({
      tenant_id: storageRetryPayload.tenant_id,
      target_storage_bytes: storageRetryPayload.target_storage_bytes,
      session_cancel_url: errorUrl,
      session_success_url: successUrl,
    })
      .then(async ({ data: res }) => {
        if (res?.code !== 0) {
          throw new Error(res?.message || t('billing.storageUpgradeFailed'));
        }

        localStorage.removeItem(StorageAddonSetupRetryKey);
        await invalidateBillingQueries();

        if (res.data?.redirect_to) {
          openPaymentRedirectPreservingPage(res.data.redirect_to);
          return;
        }

        message.success(t('price.paymentSuccessfulTip'));
      })
      .catch((error) => {
        localStorage.removeItem(StorageAddonSetupRetryKey);
        message.error(
          (error as Error)?.message || t('billing.storageUpgradeFailed'),
        );
      });
  }, [checkout, queryClient, status, t]);
};

const useFetchCurrentPlan = (force = false) => {
  const user = storage.getUserInfo();
  const { data, isFetching: loading } = useQuery<ICurrentPlan>({
    queryKey: [BillingQueryKey.CurrentPlan],
    // initialData: {},
    enabled: !!user && !!isBillingEnabled(),
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.getCurrentPlan();
      if (res.code === 0) {
        const { data } = res;
        storagePrivate.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading };
};

const useFetchPlanList = (force = false) => {
  const { data, isFetching: loading } = useQuery<IPlan[]>({
    queryKey: [BillingQueryKey.PlanList],
    // initialData: {},
    gcTime: force ? 0 : 50000,
    queryFn: async () => {
      const { data: res } = await billingService.getPlanList();
      if (res.code === 0) {
        const { data } = res;
        // storage.setPricePlan(JSON.stringify(data));
        return data;
      }
    },
  });

  return { data, loading };
};

const getNextMonth = {
  getNextMonthFirstDay: () => {
    const today = new Date();
    const nextMonth = new Date(today.getFullYear(), today.getMonth() + 1, 1);
    return nextMonth;
  },

  get31Day: () => {
    const today = new Date();
    const futureDate = new Date(today.getTime() + 31 * 24 * 60 * 60 * 1000);
    return futureDate;
  },

  getDayFormatted: (date: Date) => {
    const thisDate = date;
    const year = thisDate.getFullYear();
    const month = String(thisDate.getMonth() + 1).padStart(2, '0');
    const day = String(thisDate.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
  },
};
export { getNextMonth, useCharge, useFetchCurrentPlan, useFetchPlanList };
