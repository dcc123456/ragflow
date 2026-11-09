import { useFetchAgent } from '@/hooks/use-agent-request';

export const useFetchCanvasPresence = () => {
  const { data: agent } = useFetchAgent();

  // Editing permissions come from agent details without a presence request.
  return {
    data: { operator_permission: agent.operator_permission ?? 0 },
  };
};
