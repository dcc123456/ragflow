import useGraphStore from '@/pages/agent/store';
import { getAgentNodeMCP } from '@/pages/agent/utils';
import { useMemo } from 'react';

export function useValues() {
  const { clickedToolId, clickedNodeId, findUpstreamNodeById } = useGraphStore(
    (state) => state,
  );

  const values = useMemo(() => {
    const agentNode = findUpstreamNodeById(clickedNodeId);
    const mcpList = getAgentNodeMCP(agentNode);

    const formData = mcpList.find((x) => x.mcp_id === clickedToolId);

    const tools = formData?.tools || {};
    const headers = formData?.headers || {};

    const res = { items: Object.keys(tools) || [], headers: headers };
    return res;
  }, [clickedNodeId, clickedToolId, findUpstreamNodeById]);

  return values;
}
