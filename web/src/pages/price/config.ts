import { PriceName } from './constant';

// A pop-up window appears after viewing a certain number of pages.
export const freePageNumber = 20;

// Price per GB of knowledge base USD
export const pricePerGB = 0.15;

// Price per 100 pages parsed USD
export const pricePer100Pages = 0.0001;

export const priceIdConfig = {
  [PriceName.Trial]: 'price_1RWUhlPtsKvwvC5fJHfaYeRs',
  [PriceName.Starter]: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
  [PriceName.Pro]: 'price_1RSr42PtsKvwvC5fuZP0AH7B',
  [PriceName.Enterprise]: 'Enterprise',
};
