import { PrivateRoutes } from '@/private-routes';
import { useCallback } from 'react';
import { useNavigate } from 'react-router';

export const useEnterpriseNavigate = () => {
  const navigate = useNavigate();

  const navigateToTickets = useCallback(() => {
    navigate(PrivateRoutes.Tickets);
  }, [navigate]);

  const navigateToTicketCreate = useCallback(() => {
    navigate(PrivateRoutes.TicketCreate);
  }, [navigate]);

  const navigateToTicketDetail = useCallback(
    (id: number | string) => () => {
      navigate(`${PrivateRoutes.Tickets}/${id}`);
    },
    [navigate],
  );

  return {
    navigateToTickets,
    navigateToTicketCreate,
    navigateToTicketDetail,
  };
};
