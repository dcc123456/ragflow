import { AxiosInstance } from 'axios';
import get from 'lodash/get';
import { RequestMethod } from 'umi-request';
import api, { restAPIv1 } from './api';

type Event = (...params: any[]) => unknown;

export class Channel {
  private static instance: Channel;
  private constructor() {}

  static getInstance() {
    if (!Channel.instance) {
      Channel.instance = new Channel();
    }
    return Channel.instance;
  }

  listenersContainer: Record<string, Array<Event>> = {};

  on(name: string, event: Event) {
    if (Array.isArray(this.listenersContainer[name])) {
      this.listenersContainer[name].push(event);
    } else {
      this.listenersContainer[name] = [event];
    }
  }
  emit(name: string, ...params: unknown[]) {
    if (Array.isArray(this.listenersContainer[name])) {
      this.listenersContainer[name].forEach((event) => {
        event(...params);
      });
    }
  }
  cancel(name: string) {
    this.listenersContainer[name].length = 0;
  }
}

const ExactListeners: string[] = [
  api.document_upload,
  api.chunk_list,
  api.create_kb,
  api.createChat,
  api.setCanvas,
  api.runCanvas,
  api.retrieval_test,
  api.chatsRelatedQuestions,
  api.kb_list,
];

const ChatDetailPattern = new RegExp(`^${restAPIv1}/chats/[^/]+$`);

const shouldShowStar = (url?: string, method?: string) => {
  if (!url) {
    return false;
  }

  if (ExactListeners.includes(url)) {
    return true;
  }

  return (
    ChatDetailPattern.test(url) &&
    ['put', 'patch'].includes((method ?? '').toLowerCase())
  );
};

export function showStarModal(
  url: string,
  method: string | undefined,
  request: RequestMethod,
) {
  if (shouldShowStar(url, method)) {
    request.get('/v1/user/star').then((ret) => {
      const star = get(ret, 'data.data.star');
      if (star === false) {
        Channel.getInstance().emit('star', true);
      }
    });
  }
}

export function showStarDialog(
  request: AxiosInstance,
  url?: string,
  method?: string,
) {
  if (shouldShowStar(url, method)) {
    request.get('/v1/user/star').then((ret) => {
      const star = get(ret, 'data.data.star');
      if (star === false) {
        Channel.getInstance().emit('star', true);
      }
    });
  }
}
