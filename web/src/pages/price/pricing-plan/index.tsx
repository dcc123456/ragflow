// pages/PricingPage.tsx
import { LoadingButton } from '@/components/ui/loading-button';
import { Modal } from '@/components/ui/modal/modal';
import { convertBytesToGb } from '@/lib/utils';
import { cancelScheduledSubscriptionChange } from '@/services/price';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { t } from 'i18next';
import { Building2, Gem, LucideProps, Rocket, X } from 'lucide-react';
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { JSX } from 'react/jsx-runtime';
import PricingCard from '../components/pricing-card';
import { priceIdConfig } from '../config';
import { PriceName } from '../constant';
import { useFetchCurrentPlan, useFetchPlanList } from '../hook/use-price-hooks';
import { IPricePlanWithButton } from '../interface';
import { showModal } from '../price-modal/show-modal';

const UNLIMITED_API_REQUESTS = 2147483647;

const formatApiRequests = (limit: number) =>
  limit >= UNLIMITED_API_REQUESTS ? 'Unlimited' : `${limit}/month`;

const pricingPlans = {
  [PriceName.Trial]: {
    id: priceIdConfig[PriceName.Trial],
    title: t('price.free'),
    description: t('price.freeDesc'),
    price: '',
    feature: {
      apps: '',
      teamMembers: '',
      datasetStorage: '',
      apiRequests: '',
    },
    buttonLabel: t('price.reduce'),
    isUse: true,
    icon: () => <></>,
  },
  [PriceName.Starter]: {
    id: priceIdConfig[PriceName.Starter],
    title: t('price.starter'),
    description: t('price.starterDesc'),
    price: '',
    feature: {
      apps: '',
      teamMembers: '',
      datasetStorage: '',
      apiRequests: '',
    },
    buttonLabel: t('price.upgrade'),
    isUse: false,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Rocket {...props} />;
    },
  },
  [PriceName.Pro]: {
    id: priceIdConfig[PriceName.Pro],
    title: t('price.pro'),
    description: t('price.proDesc'),
    price: '',
    feature: {
      apps: '',
      teamMembers: '',
      datasetStorage: '',
      apiRequests: '',
    },
    buttonLabel: t('price.upgrade'),
    isUse: false,
    isPopular: true,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Gem {...props} />;
    },
  },
  [PriceName.Enterprise]: {
    id: priceIdConfig[PriceName.Enterprise],
    title: t('price.enterprise'),
    description: t('price.enterpriseDesc'),
    price: '?',
    feature: {
      apps: '?',
      teamMembers: '?',
      datasetStorage: '?',
      apiRequests: '?',
    },
    buttonLabel: t('price.contactUs'),
    isUse: false,
    icon: (
      props?: JSX.IntrinsicAttributes &
        Omit<LucideProps, 'ref'> &
        React.RefAttributes<SVGSVGElement>,
    ) => {
      return <Building2 {...props} />;
    },
  },
};

const PricingPlan = ({ isUpgrade = false }: { isUpgrade: boolean }) => {
  const { data: currentPlan } = useFetchCurrentPlan();
  const { data: planList } = useFetchPlanList();
  const queryClient = useQueryClient();
  const [pricePlanList, setPricePlanList] = useState<IPricePlanWithButton[]>();
  const urlParams = useMemo(
    () => new URLSearchParams(window.location.search),
    [],
  );
  // const [searchParams, setSearchParams] = useSearchParams();
  const status = urlParams.get('price-pay-status');
  const { t } = useTranslation();

  const { mutateAsync: cancelDowngrade, isPending: cancelingDowngrade } =
    useMutation({
      mutationKey: ['cancelScheduledDowngrade'],
      mutationFn: async () => {
        if (!currentPlan?.tenant_id) return;
        const { data: res } = await cancelScheduledSubscriptionChange(
          currentPlan.tenant_id,
        );
        if (res.code === 0) return res.data;
        throw new Error(res.message || 'Failed to cancel scheduled downgrade');
      },
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: ['currentPlan'] });
      },
    });

  const openSuccessModal = useCallback(
    (status: string) => {
      const title = () => {
        switch (status) {
          case 'success':
            return (
              <div className="flex gap-2 items-center">
                {t('price.paymentSuccessful')}
              </div>
            );
          case 'cancel':
            return (
              <div className="flex gap-2 items-center">
                <div className="p-1 w-5 h-5 flex items-center justify-center rounded-full bg-red-500">
                  <X size={14} fontWeight={'bold'} />
                </div>
                {t('price.paymentFailed')}
              </div>
            );
          default:
            return 'Success';
        }
      };
      const content = () => {
        switch (status) {
          case 'success':
            return (
              <div>
                <div className="flex items-center gap-2">
                  {t('price.paymentSuccessfulTip')}
                </div>
              </div>
            );
          case 'error':
            return (
              <div>
                <div className="flex items-center gap-2">
                  {t('price.paymentFailedTip')}
                </div>
              </div>
            );
          default:
            return 'Success';
        }
      };
      if (status) {
        // searchParams.delete('status');
        // setSearchParams(searchParams);
        const successModal = showModal({
          children: (
            <Modal
              open={true}
              title={title()}
              onOpenChange={(open) => {
                if (!open) {
                  const urlObj = new URL(window.location.href);
                  urlObj.searchParams.delete('price-pay-status');
                  window.history.replaceState({}, '', urlObj.toString());
                  successModal.destroy();
                }
              }}
              className="!w-[400px]"
              footer={
                <div className="flex justify-end gap-2 ">
                  <button
                    type="button"
                    onClick={() => {
                      const urlObj = new URL(window.location.href);
                      urlObj.searchParams.delete('price-pay-status');
                      window.history.replaceState({}, '', urlObj.toString());
                      successModal.destroy();
                    }}
                    className="px-2 py-1 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
                  >
                    {t('modal.okText')}
                  </button>
                </div>
              }
            >
              <div className="h-32">{content()}</div>
            </Modal>
          ),
        });
      }
    },
    [urlParams, t],
  );

  useEffect(() => {
    if (!currentPlan || !planList || planList.length <= 0) return;
    let inUseIndex = 4;

    let plans = planList?.map((plan, index) => {
      let tempPlan = {
        ...pricingPlans[plan.name as keyof typeof pricingPlans],
        name: plan.name,
        feature: {
          apps: plan.feature.quota_apps,
          teamMembers: plan.feature.quota_members,
          datasetStorage: convertBytesToGb(plan.feature.quota_kb_storage),
          apiRequests: formatApiRequests(plan.feature.quota_api_limits),
        },
        id: plan.price_ids,
        price: plan.price,
        isUse: false,
      };

      if (plan.name && currentPlan.plan_name === plan.name) {
        inUseIndex = index;
        return {
          ...tempPlan,
          isUse: true,
          buttonLabel: t('price.inUse'),
        };
      } else {
        const buttonLabel =
          index < inUseIndex
            ? t('price.reduce')
            : index < planList.length - 1
              ? t('price.upgrade')
              : t('price.contactUs');
        return {
          ...tempPlan,
          buttonLabel,
        };
      }
    });

    if (isUpgrade) {
      plans = plans.filter((plan) => plan.name !== PriceName.Trial);
    }
    setPricePlanList(plans as unknown as IPricePlanWithButton[]);
  }, [currentPlan, planList, t, isUpgrade]);

  useEffect(() => {
    if (status) {
      openSuccessModal(status);
    }
  }, [status, openSuccessModal]);

  //   showPriceModal(ref);
  return (
    <>
      {currentPlan?.pending_subscription_change?.schedule_id && (
        <div className="mb-4 p-4 rounded-md border border-border-default bg-bg-card flex items-center justify-between">
          <div className="text-sm">
            Downgrade scheduled to{' '}
            <span className="font-medium">
              {currentPlan.pending_subscription_change.pending_plan_name ||
                'the selected plan'}
            </span>
            {currentPlan.pending_subscription_change.effective_at
              ? ` at ${currentPlan.pending_subscription_change.effective_at}`
              : ' at period end'}
            .
          </div>
          <LoadingButton
            size="sm"
            variant="outline"
            loading={cancelingDowngrade}
            onClick={async () => {
              try {
                await cancelDowngrade();
                const modal = showModal({
                  children: (
                    <Modal
                      open={true}
                      title="Downgrade canceled"
                      onOpenChange={(open) => {
                        if (!open) modal.destroy();
                      }}
                      className="!w-[400px]"
                    >
                      <div className="h-20">
                        Your scheduled downgrade has been canceled.
                      </div>
                    </Modal>
                  ),
                });
              } catch (e: any) {
                const modal = showModal({
                  children: (
                    <Modal
                      open={true}
                      title="Cancel failed"
                      onOpenChange={(open) => {
                        if (!open) modal.destroy();
                      }}
                      className="!w-[400px]"
                    >
                      <div className="h-20">
                        {e?.message || 'Failed to cancel scheduled downgrade.'}
                      </div>
                    </Modal>
                  ),
                });
              }
            }}
          >
            Cancel downgrade
          </LoadingButton>
        </div>
      )}
      {pricePlanList?.map((plan, index) => (
        <PricingCard key={index} {...plan} />
      ))}
    </>
  );
};

export default PricingPlan;
