import NumberInput from '@/components/originui/number-input';
import { BillingQueryKey } from '@/pages/billing/constants/query-keys';
import { getBillingPointsPrice } from '@/services/price';
import { useQuery } from '@tanstack/react-query';
import React, { useState } from 'react';
import { useFetchAddonPlans } from '../hook/use-addon-plans';

const AddOnCalculator: React.FC = () => {
  const [quantities, setQuantities] = useState<{ [key: string]: number }>({});
  const { data: pointsPriceData } = useQuery({
    queryKey: [BillingQueryKey.PointsPrice],
    queryFn: async () => {
      const { data: res } = await getBillingPointsPrice();
      return res?.code === 0 ? res.data : null;
    },
  });
  const { pricePerGB: pricePerGBFromApi } = useFetchAddonPlans();

  const pricePer100Pages = pointsPriceData?.price_usd
    ? pointsPriceData.price_usd / (pointsPriceData.points_per_unit / 100)
    : 0;

  const products = [
    {
      name: 'Storage',
      unit: 'GB',
      pricePerUnit: pricePerGBFromApi,
      per: '/month',
      step: 1,
    },
    {
      name: 'Document Parsing',
      unit: 'page',
      pricePerUnit: pricePer100Pages,
      per: '',
      step: 100,
    },
  ];

  const handleQuantityChange = (productName: string, value: number) => {
    setQuantities({ ...quantities, [productName]: value });
  };

  const totalPrice = (product: (typeof products)[0]) => {
    const total = (quantities[product.name] || 0) * product.pricePerUnit;
    return total.toFixed(2);
  };
  return (
    <div className="mt-16">
      <h2 className="text-2xl font-bold mb-4">Add-on Calculator</h2>
      <p className="mb-6">
        Estimate the cost of add-on services tailored to your needs.
      </p>
      <div className="rounded-lg overflow-hidden border border-border-default">
        <table
          className="w-full rounded-lg bg-bg-input border border-border-button"
          cellPadding={15}
        >
          <thead className="text-left text-base text-text-secondary bg-bg-title rounded-lg ">
            <tr>
              <th className="py-2 font-normal">Product</th>
              <th className="py-2 font-normal">Plan</th>
              <th className="py-2 font-normal">Price</th>
            </tr>
          </thead>
          <tbody className="text-left p-4 ">
            {products.map((product) => (
              <tr
                key={product.name}
                className="border-border-default border-t-[1px] "
              >
                <td className="w-1/3 py-4 text-text-primary">{product.name}</td>
                <td className="py-4">
                  <div className="flex items-center gap-2">
                    <NumberInput
                      className="w-1/3"
                      step={product.step}
                      value={quantities[product.name] || 0}
                      onChange={(e) => handleQuantityChange(product.name, e)}
                      height={40}
                    />
                    {product.unit}
                  </div>
                </td>
                <td className="w-1/3 py-4">
                  <span className="text-text-primary text-2xl font-bold">
                    ${totalPrice(product)}
                  </span>
                  <span className="text-text-secondary text-sm ml-2">
                    {product.per}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};

export default AddOnCalculator;
