const storagePrivate = {
  setPricePlan: (pricePlan: string | object) => {
    if (typeof pricePlan === 'object') {
      const plan = JSON.stringify(pricePlan);
      localStorage.setItem('price-plan', plan);
    } else {
      localStorage.setItem('price-plan', pricePlan);
    }
  },
  getPricePlan: (): object => {
    const plan = localStorage.getItem('price-plan');
    if (plan) {
      return JSON.parse(plan);
    } else {
      return {};
    }
  },
};

export default storagePrivate;
