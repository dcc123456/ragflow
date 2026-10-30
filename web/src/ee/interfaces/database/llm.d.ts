import '@/interfaces/database/llm';

declare module '@/interfaces/database/llm' {
  interface IProviderInstance {
    operator_permission?: number;
  }
}
