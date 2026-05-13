import {
  ICreateTicketRequestBody,
  IGetTicketsRequestParams,
  IReplyTicketRequestBody,
} from '@/interfaces/request/ticket';
import request from '@/utils/next-request';
import api from '@/utils/private-api';

const {
  tickets,
  ticketDetail,
  ticketArticles,
  ticketClose,
  ticketGroups,
  ticketAttachment,
} = api;

export const getTickets = (params?: IGetTicketsRequestParams) => {
  return request.get(tickets, { params });
};

export const createTicket = (data: ICreateTicketRequestBody) => {
  return request.post(tickets, data);
};

export const getTicketDetail = (id: number) => {
  return request.get(ticketDetail(id));
};

export const getTicketArticles = (id: number) => {
  return request.get(ticketArticles(id));
};

export const closeTicket = (id: number) => {
  return request.post(ticketClose(id));
};

export const replyTicket = (id: number, data: IReplyTicketRequestBody) => {
  return request.post(ticketArticles(id), data);
};

export const getTicketGroups = () => {
  return request.get(ticketGroups);
};

export const downloadTicketAttachment = (
  ticketId: number,
  articleId: number,
  attachmentId: number,
) => {
  return request.get(ticketAttachment(ticketId, articleId, attachmentId), {
    responseType: 'blob',
  });
};
