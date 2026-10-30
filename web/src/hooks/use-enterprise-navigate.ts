import { Routes } from '@/routes';
import { useCallback } from 'react';
import { useNavigate } from 'react-router';

export const useEnterpriseNavigate = () => {
  const navigate = useNavigate();

  const navigateToTickets = useCallback(() => {
    navigate(Routes.Tickets);
  }, [navigate]);

  const navigateToTicketCreate = useCallback(() => {
    navigate(Routes.TicketCreate);
  }, [navigate]);

  const navigateToTicketDetail = useCallback(
    (id: number | string) => () => {
      navigate(`${Routes.Tickets}/${id}`);
    },
    [navigate],
  );

  return {
    navigateToTickets,
    navigateToTicketCreate,
    navigateToTicketDetail,
  };
};
