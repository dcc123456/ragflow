import message from '@/components/ui/message';
import {
  ITicket,
  ITicketArticleAttachment,
} from '@/interfaces/database/ticket';
import { ICreateTicketRequestBody } from '@/interfaces/request/ticket';
import {
  closeTicket,
  createTicket,
  getTicketArticles,
  getTicketDetail,
  getTicketGroups,
  getTickets,
  replyTicket,
} from '@/services/ticket-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useDebounce } from 'ahooks';
import {
  useGetPaginationWithRouter,
  useHandleSearchChange,
} from './logic-hooks';

export const enum TicketApiAction {
  FetchTickets = 'fetchTickets',
  CreateTicket = 'createTicket',
  CloseTicket = 'closeTicket',
  ReplyTicket = 'replyTicket',
  FetchTicketDetail = 'fetchTicketDetail',
  FetchTicketArticles = 'fetchTicketArticles',
  FetchTicketGroups = 'fetchTicketGroups',
}

export interface ITicketGroup {
  id: number;
  name: string;
  name_last: string;
  active: boolean;
}

export const useFetchTicketGroups = () => {
  const { data, isFetching: loading } = useQuery<{
    data: ITicketGroup[];
  }>({
    queryKey: [TicketApiAction.FetchTicketGroups],
    queryFn: async () => {
      const { data } = await getTicketGroups();
      return data;
    },
  });

  return {
    groups: data?.data ?? [],
    loading,
  };
};

export const useFetchTickets = () => {
  const { searchString, handleInputChange } = useHandleSearchChange();
  const { pagination, setPagination } = useGetPaginationWithRouter();
  const debouncedSearchString = useDebounce(searchString, { wait: 500 });

  const { data, isFetching: loading } = useQuery<{
    tickets: ITicket[];
    total: number;
  }>({
    queryKey: [TicketApiAction.FetchTickets, debouncedSearchString, pagination],
    initialData: { tickets: [], total: 0 },
    queryFn: async () => {
      const { data } = await getTickets({
        page: pagination.current,
        page_size: pagination.pageSize,
        keywords: debouncedSearchString,
      });
      return {
        tickets: data?.data?.list ?? [],
        total: data?.data?.total ?? 0,
      };
    },
  });

  return {
    loading,
    searchString,
    tickets: data.tickets,
    pagination: { ...pagination, total: data.total },
    handleInputChange,
    setPagination,
  };
};

export const useCreateTicket = () => {
  const queryClient = useQueryClient();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TicketApiAction.CreateTicket],
    mutationFn: async (payload: ICreateTicketRequestBody) => {
      const { data } = await createTicket(payload);
      return data;
    },
    onSuccess: () => {
      message.success('success');
      queryClient.invalidateQueries({
        queryKey: [TicketApiAction.FetchTickets],
      });
    },
    onError: (error: any) => {
      message.error(error?.message || 'Failed to create ticket');
    },
  });

  return { data, loading, createTicket: mutateAsync };
};

export const useCloseTicket = () => {
  const queryClient = useQueryClient();

  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: [TicketApiAction.CloseTicket],
    mutationFn: async (id: number) => {
      const { data } = await closeTicket(id);
      return data;
    },
    onSuccess: () => {
      message.success('success');
      queryClient.invalidateQueries({
        queryKey: [TicketApiAction.FetchTickets],
      });
    },
    onError: (error: any) => {
      message.error(error?.message || 'Failed to close ticket');
    },
  });

  return { loading, closeTicket: mutateAsync };
};

export const useFetchTicketDetail = (id: number) => {
  const { data, isFetching: loading } = useQuery<{ data: ITicket }>({
    queryKey: [TicketApiAction.FetchTicketDetail, id],
    queryFn: async () => {
      const { data } = await getTicketDetail(id);
      return data;
    },
    enabled: id > 0,
  });

  return { data: data?.data, loading };
};

export interface ITicketArticle {
  id: number;
  subject: string;
  body: string;
  from: string | null;
  origin_by: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  sender: string;
  internal: boolean;
  attachments?: ITicketArticleAttachment[];
}

export const useFetchTicketArticles = (id: number) => {
  const { data, isFetching: loading } = useQuery<{
    data: ITicketArticle[];
  }>({
    queryKey: [TicketApiAction.FetchTicketArticles, id],
    queryFn: async () => {
      const { data } = await getTicketArticles(id);
      return data;
    },
    enabled: id > 0,
  });

  return { data: data?.data ?? [], loading };
};

export const useReplyTicket = (id: number) => {
  const queryClient = useQueryClient();

  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: [TicketApiAction.ReplyTicket, id],
    mutationFn: async (payload: {
      body: string;
      subject?: string;
      attachments?: { filename: string; data: string; 'mime-type': string }[];
    }) => {
      const { data } = await replyTicket(id, payload);
      return data;
    },
    onSuccess: () => {
      message.success('success');
      queryClient.invalidateQueries({
        queryKey: [TicketApiAction.FetchTicketArticles, id],
      });
    },
    onError: (error: any) => {
      message.error(error?.message || 'Failed to send reply');
    },
  });

  return { loading, replyTicket: mutateAsync };
};
