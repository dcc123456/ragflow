import { canEditAgent } from '@/ee/pages/agent/can-edit-agent';
import { ReactNode } from 'react';

type AgentEditGuardProps = {
  operatorPermission: number;
  children: ReactNode;
};

/**
 * Hides the agent canvas editing controls from operators without write access.
 */
export function AgentEditGuard({
  operatorPermission,
  children,
}: AgentEditGuardProps) {
  if (!canEditAgent(operatorPermission)) {
    return null;
  }

  return <>{children}</>;
}
