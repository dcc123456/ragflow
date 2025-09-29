export interface IFeatureProps {
  apps: string;
  teamMembers: string;
  datasetStorage: string;
  apiRequests: string;
}
export interface IPricePlan {
  id: string;
  title: string;
  description: string;
  price: string;
  feature: IFeatureProps;
}

export type IPricePlanWithButton = IPricePlan & {
  buttonLabel: string;
  isUse: boolean;
  icon: () => JSX.Element;
};
