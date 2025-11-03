import { ICurrentPlan } from '@/pages/price/interface';

const storagePrivate = {
  setPricePlan: (pricePlan: string | ICurrentPlan) => {
    if (typeof pricePlan === 'object') {
      const plan = JSON.stringify(pricePlan);
      localStorage.setItem('price-plan', plan);
    } else {
      localStorage.setItem('price-plan', pricePlan);
    }
  },
  getPricePlan: (): ICurrentPlan => {
    const plan = localStorage.getItem('price-plan');
    if (plan) {
      return JSON.parse(plan);
    } else {
      return {} as ICurrentPlan;
    }
  },
};

export default storagePrivate;
