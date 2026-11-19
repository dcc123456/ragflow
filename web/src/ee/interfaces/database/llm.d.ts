import '@/interfaces/database/llm';

declare module '@/interfaces/database/llm' {
  interface IProviderInstance {
    user_id?: string;
    nickname?: string;
    operator_permission?: number;
  }
}
