export interface ITicketArticleAttachment {
  id: number;
  filename: string;
  size: number;
  preferences: {
    'Content-Type'?: string;
    'content-type'?: string;
  };
}

export interface ITicket {
  id: number;
  title: string;
  state: string;
  priority: string;
  group: string;
  customer: string;
  created_at: string;
  updated_at: string;
}

export interface ITicketListResponse {
  list: ITicket[];
  total: number;
}
