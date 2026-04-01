// Type definitions for Model Usage Statistics page

export type ViewType = 'users' | 'departments' | 'groups';

export interface FilterValues {
  view: ViewType;
  timeRange: string;
  searchValue: string;
}

export type LlmTraceDimension = 'user' | 'team' | 'dept';

export interface LlmTraceSummary {
  avg_duration_ms: number;
  input_tokens: number;
  output_tokens: number;
  total_requests: number;
  total_tokens: number;
  total_traces: number;
  unique_depts: number;
  unique_teams: number;
  unique_users: number;
}

export interface LlmTraceByOrgItem {
  rank: number;
  id: string;
  name: string;
  info: {
    email?: string;
    nickname?: string;
    avatar?: string;
    name?: string;
  };
  team_info?: Array<{ name: string; avatar: string }>;
  dept_info?: Array<{ name: string; avatar: string }>;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  request_count: number;
  avg_duration_ms: number;
  max_tokens: number;
  avg_tokens_per_request: number;
  user_count?: number;
}

export interface LlmTraceRecent {
  trace_id: string;
  span_name: string;
  timestamp: string;
  user_info: {
    email: string;
    nickname?: string;
    avatar?: string;
  };
  dept_info: Array<{ name: string; avatar: string }>;
  team_info: Array<{ name: string; avatar: string }>;
  model: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  duration_ms: number;
}

export interface LlmTraceTopConsumer {
  rank: number;
  name: string;
  dimension: LlmTraceDimension;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  request_count: number;
  avg_duration_ms: number;
}

export interface LlmTraceTrend {
  time: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  request_count: number;
  avg_duration_ms: number;
}

export interface LlmTraceByModelItem {
  model_name: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  request_count: number;
  avg_duration_ms: number;
}
