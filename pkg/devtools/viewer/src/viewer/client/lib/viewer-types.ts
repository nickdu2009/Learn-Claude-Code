export interface TraceMeta {
  supported: boolean;
  version: number;
  generated_at?: string;
  message?: string;
}

export interface RunNode {
  id: string;
  kind: string;
  title: string;
  status: string;
  completion_reason?: string | null;
  started_at: string;
  finished_at?: string | null;
  step_count: number;
  summary?: string | null;
  input_preview?: string | null;
  child_count: number;
  children: RunNode[];
}

export interface RunRecord {
  id: string;
  kind: string;
  title: string;
  status: string;
  completion_reason?: string | null;
  started_at: string;
  finished_at?: string | null;
  parent_run_id?: string | null;
  parent_step_id?: string | null;
  summary?: string | null;
  input_preview?: string | null;
  step_count: number;
  error?: string | null;
}

export interface StepRecord {
  id: string;
  run_id: string;
  step_number: number;
  type: 'generate' | 'stream';
  model_id: string;
  provider: string | null;
  started_at: string;
  duration_ms: number | null;
  input: string;
  output: string | null;
  usage: string | null;
  error: string | null;
  raw_request: string | null;
  raw_response: string | null;
  raw_chunks: string | null;
  provider_options: string | null;
  linked_child_run_ids?: string[];
}

export interface RunDetail {
  run: RunRecord;
  steps: StepRecord[];
  linked_child_runs_by_step: Record<string, RunRecord[]>;
  parent?: RunRecord | null;
}

export type UnsupportedState = {
  title: string;
  message: string;
};

export type JSONRecord = Record<string, unknown>;

export type PromptMessage = {
  role: string;
  content?: unknown;
  tool_calls?: unknown[];
  tool_call_id?: string;
};

export type StepInput = JSONRecord & {
  prompt: PromptMessage[];
};

export type ParsedStepRecord = StepRecord & {
  parsedInput: StepInput;
  parsedOutput: JSONRecord | null;
  parsedUsage: JSONRecord | null;
  parsedProviderOptions: JSONRecord | null;
  parsedRawRequest: unknown | null;
  parsedRawResponse: unknown | null;
  parsedRawChunks: unknown | null;
};

export type ParsedRunDetail = {
  run: RunRecord;
  steps: ParsedStepRecord[];
  linked_child_runs_by_step: Record<string, RunRecord[]>;
  parent?: RunRecord | null;
};

export type StepSummaryInfo = {
  icon: 'message' | 'wrench' | 'alert';
  label: string;
  detail?: string;
};

export type StepInputSummary = {
  label: string;
  detail?: string;
};

export interface InputTokenBreakdown {
  total: number;
  noCache?: number;
  cacheRead?: number;
  cacheWrite?: number;
}

export interface OutputTokenBreakdown {
  total: number;
  text?: number;
  reasoning?: number;
}

export type ToolCallPart = {
  toolName?: string;
  toolCallId?: string;
  args?: unknown;
};

export type ToolResultPart = {
  toolName?: string;
  toolCallId?: string;
  result?: unknown;
};
