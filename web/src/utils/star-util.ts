import { AxiosInstance } from 'axios';
import get from 'lodash/get';
import { RequestMethod } from 'umi-request';
import api from './api';

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

const Listeners: string[] = [
  api.document_upload,
  api.chunk_list,
  api.create_kb,
  api.setDialog,
  api.setCanvas,
  api.runCanvas,
  api.retrieval_test,
  api.getRelatedQuestions,
  api.create_kb,
  api.setCanvas,
  api.runCanvas,
  api.setDialog,
  api.kb_list,
];

export function showStarModal(url: string, request: RequestMethod) {
  if (Listeners.some((x) => x === url)) {
    request.get('/v1/user/star').then((ret) => {
      const star = get(ret, 'data.data.star');
      if (star === false) {
        Channel.getInstance().emit('star', true);
      }
    });
  }
}

export function showStarDialog(request: AxiosInstance, url?: string) {
  if (Listeners.some((x) => x === url)) {
    request.get('/v1/user/star').then((ret) => {
      const star = get(ret, 'data.data.star');
      if (star === false) {
        Channel.getInstance().emit('star', true);
      }
    });
  }
}
