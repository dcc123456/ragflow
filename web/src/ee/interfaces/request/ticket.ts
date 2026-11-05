export interface ITicketAttachment {
  filename: string;
  data: string;
  'mime-type': string;
}

export interface ICreateTicketRequestBody {
  title: string;
  group: string;
  customer: string;
  article: {
    subject: string;
    body: string;
    type?: string;
    internal?: boolean;
  };
  attachments?: ITicketAttachment[];
}

export interface IReplyTicketRequestBody {
  body: string;
  subject?: string;
  attachments?: ITicketAttachment[];
}

export interface IGetTicketsRequestParams {
  page?: number;
  page_size?: number;
  keywords?: string;
}
