import { PriceNameMapValue } from '../constant';

type PlanLike =
  | {
      plan_name?: string;
      price_id?: string;
    }
  | null
  | undefined;

export const shouldPreviewPlanUpgrade = (
  currentPlan: PlanLike,
  targetPlanName?: string,
) => {
  if (!currentPlan?.plan_name || !targetPlanName) {
    return false;
  }

  const currentRank =
    PriceNameMapValue[currentPlan.plan_name as keyof typeof PriceNameMapValue];
  const targetRank =
    PriceNameMapValue[targetPlanName as keyof typeof PriceNameMapValue];

  if (currentRank === undefined || targetRank === undefined) {
    return false;
  }

  return currentRank < targetRank;
};
