// pages/PricingPage.tsx
import { ButtonLoading } from '@/components/ui/button';
import { Modal } from '@/components/ui/modal/modal';
import { convertBytesToGb } from '@/lib/utils';
import { cancelScheduledSubscriptionChange } from '@/services/price';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { t } from 'i18next';
import {
  BanknoteArrowUp,
  Coins,
  DatabaseZap,
  HeartHandshake,
  LayoutGrid,
  Loader2,
  ShieldCheck,
  Users,
  Vault,
  X,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { JSX } from 'react/jsx-runtime';
import PricingCard from '../components/pricing-card';
import { PriceName } from '../constant';
import { useFetchCurrentPlan, useFetchPlanList } from '../hook/use-price-hooks';
import { IPricePlanWithButton } from '../interface';
import { showModal } from '../price-modal/show-modal';

const UNLIMITED_API_REQUESTS = 2147483647;

const formatApiRequests = (limit: number) =>
  limit >= UNLIMITED_API_REQUESTS ? 'Unlimited' : `${limit}/month`;
const commonFeatures = [
  {
    key: 'apps',
    value: '',
    name: 'Apps',
    icon: (
      <LayoutGrid size={12} className="text-text-primary font-normal mr-2" />
    ),
  },
  {
    key: 'teamMembers',
    value: '',
    name: 'team members',
    icon: <Users size={12} className="text-text-primary font-normal mr-2" />,
  },
  {
    key: 'datasetStorage',
    value: '',
    name: 'GB dataset storage',
    icon: (
      <DatabaseZap size={12} className="text-text-primary font-normal mr-2" />
    ),
  },
  {
    key: 'credits',
    value: '',
    name: 'credits / month',
    icon: <Coins size={12} className="text-text-primary font-normal mr-2" />,
  },
];
const pricingPlans = {
  [PriceName.Trial]: {
    id: '',
    title: t('price.free'),
    description: t('price.freeDesc'),
    price: '',
    buttonLabel: t('price.reduce'),
    isUse: true,
    features: commonFeatures,
  },
  [PriceName.Starter]: {
    id: '',
    title: t('price.starter'),
    description: t('price.starterDesc'),
    price: '',
    buttonLabel: t('price.upgrade'),
    isUse: false,
    features: commonFeatures,
  },
  [PriceName.Pro]: {
    id: '',
    title: t('price.pro'),
    description: t('price.proDesc'),
    price: '',
    buttonLabel: t('price.upgrade'),
    isUse: false,
    isPopular: true,
    features: commonFeatures,
  },
  [PriceName.Enterprise]: {
    id: '',
    title: t('price.enterprise'),
    description: t('price.enterpriseDesc'),
    price: '',
    buttonLabel: t('price.contactUs'),
    isUse: false,
    features: [
      {
        key: 'apps',
        value: '',
        name: 'BYOC deployment',
        icon: (
          <BanknoteArrowUp
            size={12}
            className="text-text-primary font-normal mr-2"
          />
        ),
      },
      {
        key: 'teamMembers',
        value: '',
        name: 'On-premises deployment',
        icon: (
          <Vault size={12} className="text-text-primary font-normal mr-2" />
        ),
      },
      {
        key: 'datasetStorage',
        value: '',
        name: 'Dedicated support',
        icon: (
          <HeartHandshake
            size={12}
            className="text-text-primary font-normal mr-2"
          />
        ),
      },
      {
        key: 'credits',
        value: '',
        name: 'Custom SLA',
        icon: (
          <ShieldCheck
            size={12}
            className="text-text-primary font-normal mr-2"
          />
        ),
      },
    ],
  },
};

const PricingPlan = ({ isUpgrade = false }: { isUpgrade: boolean }) => {
  const { data: currentPlan } = useFetchCurrentPlan();
  const { data: planList, loading } = useFetchPlanList();
  const queryClient = useQueryClient();
  const [pricePlanList, setPricePlanList] = useState<IPricePlanWithButton[]>();
  const urlParams = useMemo(
    () => new URLSearchParams(window.location.search),
    [],
  );
  // const [searchParams, setSearchParams] = useSearchParams();
  const status = urlParams.get('price-pay-status');
  const { t } = useTranslation();
  const [successModal, setSuccessModal] = useState<{
    title: string | JSX.Element;
    content: string | JSX.Element;
    open: boolean;
  }>({
    title: '',
    content: '',
    open: false,
  });

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
        setSuccessModal({
          title: title(),
          content: content(),
          open: true,
        });
        // searchParams.delete('status');
        // setSearchParams(searchParams);
        // const successModal = showModal({
        //   children: (
        //     <Modal
        //       open={true}
        //       title={title()}
        //       onOpenChange={(open) => {
        //         if (!open) {
        //           const urlObj = new URL(window.location.href);
        //           urlObj.searchParams.delete('price-pay-status');
        //           window.history.replaceState({}, '', urlObj.toString());
        //           successModal.destroy();
        //         }
        //       }}
        //       className="!w-[400px]"
        //       footer={
        //         <div className="flex justify-end gap-2 ">
        //           <button
        //             type="button"
        //             onClick={() => {
        //               const urlObj = new URL(window.location.href);
        //               urlObj.searchParams.delete('price-pay-status');
        //               window.history.replaceState({}, '', urlObj.toString());
        //               successModal.destroy();
        //             }}
        //             className="px-2 py-1 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
        //           >
        //             {t('modal.okText')}
        //           </button>
        //         </div>
        //       }
        //     >
        //       <div className="h-32">{content()}</div>
        //     </Modal>
        //   ),
        // });
      }
    },
    [t],
  );

  useEffect(() => {
    if (!currentPlan || !planList || planList.length <= 0) return;
    let inUseIndex = 4;

    let plans = planList?.map((plan, index) => {
      const featureValue = {
        apps: plan.feature.quota_apps,
        teamMembers: plan.feature.quota_members,
        datasetStorage: convertBytesToGb(plan.feature.quota_kb_storage),
        apiRequests: formatApiRequests(plan.feature.quota_api_limits),
      };
      const thisPricePlan =
        pricingPlans[plan.name as keyof typeof pricingPlans];
      const tempPlan = {
        ...thisPricePlan,
        name: plan.name,
        id: plan.price_ids,
        price: plan.price,
        isUse: false,
        features: thisPricePlan.features.map((feature) => {
          return {
            ...feature,
            value: featureValue[feature.key as keyof typeof featureValue],
          };
        }),
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
      {(loading || !pricePlanList) && (
        <div className="flex justify-center items-center h-[200px] w-full">
          <Loader2 className="animate-spin" />
        </div>
      )}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-10">
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
            <ButtonLoading
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
                          {e?.message ||
                            'Failed to cancel scheduled downgrade.'}
                        </div>
                      </Modal>
                    ),
                  });
                }
              }}
            >
              Cancel downgrade
            </ButtonLoading>
          </div>
        )}
        {!loading &&
          pricePlanList?.map((plan, index) => (
            <PricingCard key={index} {...plan} />
          ))}

        {successModal.open && (
          <Modal
            open={true}
            title={successModal.title}
            onOpenChange={(open) => {
              if (!open) {
                const urlObj = new URL(window.location.href);
                urlObj.searchParams.delete('price-pay-status');
                window.history.replaceState({}, '', urlObj.toString());
                // successModal.destroy();
                setSuccessModal({
                  open: false,
                  title: '',
                  content: '',
                });
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
                    setSuccessModal({
                      open: false,
                      title: '',
                      content: '',
                    });
                    // successModal.destroy();
                  }}
                  className="px-2 py-1 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
                >
                  {t('modal.okText')}
                </button>
              </div>
            }
          >
            <div className="h-32">{successModal.content}</div>
          </Modal>
        )}
      </div>
    </>
  );
};

export default PricingPlan;
