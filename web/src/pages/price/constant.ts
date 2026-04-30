import { FeatureItem } from './components/ComparisonTable';

export enum PriceName {
  Trial = 'Trial',
  Starter = 'Starter',
  Pro = 'Pro',
  Enterprise = 'Enterprise',
}

export const PriceNameMapValue = {
  [PriceName.Trial]: 0,
  [PriceName.Starter]: 1,
  [PriceName.Pro]: 2,
  [PriceName.Enterprise]: 3,
};

export const features: FeatureItem[] = [
  {
    name: 'Team Members',
    free: '-',
    starter: true,
    pro: true,
    enterprise: 'Unlimited',
  },
  {
    name: 'Storage',
    free: '5GB',
    starter: '50GB',
    pro: '200GB',
    enterprise: 'Unlimited',
  },
  {
    name: 'API Requests',
    free: '1K/day',
    starter: '10K/day',
    pro: '50K/day',
    enterprise: 'Unlimited',
  },
];
