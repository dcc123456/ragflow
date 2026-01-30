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
