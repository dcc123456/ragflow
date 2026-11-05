import { useCallback } from 'react';
import { useNavigate } from 'react-router';

import { EvaluationType } from '@/ee/constants/evaluation';
import { FileTabs } from '@/ee/pages/files/constant';
import { PrivateRoutes } from '@/ee/private-routes';
import { Routes } from '@/routes';

/** Enterprise-only navigation targets for evaluation pages. */
export const useEvaluationNavigate = () => {
  const navigate = useNavigate();

  const navigateToEvaluation = useCallback(
    (type: EvaluationType, id: string) => () => {
      navigate(`${PrivateRoutes.Evaluation}/${id}?type=${type}`);
    },
    [navigate],
  );

  const navigateToFileManagerEvaluationDetail = useCallback(
    (id: string) => {
      navigate(`${Routes.Files}${PrivateRoutes.EvaluationDetail}/${id}`);
    },
    [navigate],
  );

  const navigateToFileManagerEvaluation = useCallback(
    (type?: FileTabs) => {
      navigate(`${Routes.Files}?type=${type}`);
    },
    [navigate],
  );

  return {
    navigateToEvaluation,
    navigateToFileManagerEvaluation,
    navigateToFileManagerEvaluationDetail,
  };
};
