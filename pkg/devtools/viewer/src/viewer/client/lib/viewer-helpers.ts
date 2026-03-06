import type {
  InputTokenBreakdown,
  JSONRecord,
  OutputTokenBreakdown,
  ParsedRunDetail,
  ParsedStepRecord,
  PromptMessage,
  RunDetail,
  RunNode,
  RunRecord,
  StepInput,
  StepInputSummary,
  StepRecord,
  StepSummaryInfo,
  ToolCallPart,
  ToolResultPart,
} from '@/lib/viewer-types';

export function normalizeRunDetail(detail: RunDetail): ParsedRunDetail {
  if (!isRecord(detail)) {
    throw new Error('run detail payload must be an object');
  }
  validateRunRecord(detail.run, 'run detail');
  if (!Array.isArray(detail.steps)) {
    throw new Error('run detail steps must be an array');
  }
  if (!isRecord(detail.linked_child_runs_by_step)) {
    throw new Error('linked_child_runs_by_step must be an object');
  }

  const linkedChildRunsByStep: Record<string, RunRecord[]> = {};
  Object.entries(detail.linked_child_runs_by_step).forEach(([stepID, runs]) => {
    if (!Array.isArray(runs)) {
      throw new Error(`linked child runs for step ${stepID} must be an array`);
    }
    runs.forEach((run, index) =>
      validateRunRecord(run, `linked child run ${index + 1} for step ${stepID}`),
    );
    linkedChildRunsByStep[stepID] = runs as RunRecord[];
  });

  if (detail.parent != null) {
    validateRunRecord(detail.parent, 'parent run');
  }

  return {
    run: detail.run,
    steps: detail.steps.map((step, index) =>
      normalizeStepRecord(step, `step ${index + 1}`),
    ),
    linked_child_runs_by_step: linkedChildRunsByStep,
    parent: detail.parent ?? null,
  };
}

export function normalizeStepRecord(
  step: StepRecord,
  label: string,
): ParsedStepRecord {
  if (!isRecord(step)) {
    throw new Error(`${label} must be an object`);
  }
  requireString(step.id, `${label}.id`);
  requireString(step.run_id, `${label}.run_id`);
  requireString(step.model_id, `${label}.model_id`);
  requireString(step.started_at, `${label}.started_at`);
  if (step.type !== 'generate' && step.type !== 'stream') {
    throw new Error(`${label}.type must be generate or stream`);
  }

  const parsedInput = parseStepInput(step.input, `${label}.input`);
  const parsedOutput = parseOptionalRecord(step.output, `${label}.output`);
  const parsedUsage = parseOptionalRecord(step.usage, `${label}.usage`);
  const parsedProviderOptions = parseOptionalRecord(
    step.provider_options,
    `${label}.provider_options`,
  );
  const parsedRawRequest = parseOptionalJSON(step.raw_request, `${label}.raw_request`);
  const parsedRawResponse = parseOptionalJSON(
    step.raw_response,
    `${label}.raw_response`,
  );
  const parsedRawChunks = parseOptionalJSON(step.raw_chunks, `${label}.raw_chunks`);

  if (
    step.linked_child_run_ids != null &&
    !Array.isArray(step.linked_child_run_ids)
  ) {
    throw new Error(`${label}.linked_child_run_ids must be an array`);
  }

  return {
    ...step,
    parsedInput,
    parsedOutput,
    parsedUsage,
    parsedProviderOptions,
    parsedRawRequest,
    parsedRawResponse,
    parsedRawChunks,
  };
}

export function validateRunTree(runs: RunNode[], path = 'runs') {
  if (!Array.isArray(runs)) {
    throw new Error(`${path} must be an array`);
  }
  runs.forEach((run, index) => {
    validateRunRecord(run, `${path}[${index}]`);
    if (!Array.isArray(run.children)) {
      throw new Error(`${path}[${index}].children must be an array`);
    }
    validateRunTree(run.children, `${path}[${index}].children`);
  });
}

export function validateRunRecord(
  run: unknown,
  label: string,
): asserts run is RunRecord {
  if (!isRecord(run)) {
    throw new Error(`${label} must be an object`);
  }
  requireString(run.id, `${label}.id`);
  requireString(run.kind, `${label}.kind`);
  requireString(run.title, `${label}.title`);
  requireString(run.status, `${label}.status`);
  requireString(run.started_at, `${label}.started_at`);
  if (typeof run.step_count !== 'number') {
    throw new Error(`${label}.step_count must be a number`);
  }
}

export function parseStepInput(value: string, label: string): StepInput {
  const parsed = parseRequiredRecord(value, label);
  if (!Array.isArray(parsed.prompt)) {
    throw new Error(`${label}.prompt must be an array`);
  }
  return parsed as StepInput;
}

export function parseRequiredRecord(value: string, label: string): JSONRecord {
  const parsed = parseRequiredJSON(value, label);
  if (!isRecord(parsed)) {
    throw new Error(`${label} must decode to an object`);
  }
  return parsed;
}

export function parseOptionalRecord(
  value: string | null | undefined,
  label: string,
): JSONRecord | null {
  if (value == null) {
    return null;
  }
  const parsed = parseOptionalJSON(value, label);
  if (!isRecord(parsed)) {
    throw new Error(`${label} must decode to an object`);
  }
  return parsed;
}

export function parseRequiredJSON(value: string, label: string): unknown {
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`${label} must be a non-empty JSON string`);
  }
  try {
    return JSON.parse(value);
  } catch (error) {
    throw new Error(
      `${label} is not valid JSON: ${
        error instanceof Error ? error.message : 'unknown error'
      }`,
    );
  }
}

export function parseOptionalJSON(
  value: string | null | undefined,
  label: string,
): unknown | null {
  if (value == null) {
    return null;
  }
  return parseRequiredJSON(value, label);
}

export function isRecord(value: unknown): value is JSONRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function requireString(
  value: unknown,
  label: string,
): asserts value is string {
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`${label} must be a non-empty string`);
  }
}

export function getFirstUserMessage(steps: ParsedStepRecord[]): string | null {
  const firstStep = steps[0];
  if (!firstStep) {
    return null;
  }
  const userMessage = firstStep.parsedInput.prompt.find(
    message => message.role === 'user',
  );
  if (!userMessage) {
    return null;
  }
  const text = getTextContent(userMessage.content);
  return text ? truncate(text, 220) : null;
}

export function getTotalTokens(steps: ParsedStepRecord[]): {
  input: InputTokenBreakdown;
  output: OutputTokenBreakdown;
} {
  return steps.reduce(
    (acc, step) => {
      if (!step.parsedUsage) {
        return acc;
      }

      const input = getInputTokenBreakdown(
        step.parsedUsage.inputTokens as
          | number
          | InputTokenBreakdown
          | null
          | undefined,
      );
      const output = getOutputTokenBreakdown(
        step.parsedUsage.outputTokens as
          | number
          | OutputTokenBreakdown
          | null
          | undefined,
      );

      acc.input.total += input.total;
      acc.output.total += output.total;

      if (input.noCache !== undefined) {
        acc.input.noCache = (acc.input.noCache ?? 0) + input.noCache;
      }
      if (input.cacheRead !== undefined) {
        acc.input.cacheRead = (acc.input.cacheRead ?? 0) + input.cacheRead;
      }
      if (input.cacheWrite !== undefined) {
        acc.input.cacheWrite = (acc.input.cacheWrite ?? 0) + input.cacheWrite;
      }
      if (output.text !== undefined) {
        acc.output.text = (acc.output.text ?? 0) + output.text;
      }
      if (output.reasoning !== undefined) {
        acc.output.reasoning = (acc.output.reasoning ?? 0) + output.reasoning;
      }

      return acc;
    },
    {
      input: { total: 0 } as InputTokenBreakdown,
      output: { total: 0 } as OutputTokenBreakdown,
    },
  );
}

export function getStepInputSummary(
  input: StepInput,
  isFirstStep: boolean,
): StepInputSummary | null {
  const messages = input.prompt;
  if (messages.length === 0) {
    return null;
  }

  const userMessages = messages.filter(message => message.role === 'user');
  if (isFirstStep && userMessages.length > 0) {
    const message = userMessages[userMessages.length - 1];
    const text = getTextContent(message.content);
    return {
      label: 'User',
      detail: text ? truncate(text, 120) : undefined,
    };
  }

  const lastMessage = messages[messages.length - 1];
  const toolCalls = getMessageToolCalls(lastMessage);
  const toolResults = getMessageToolResults(lastMessage);
  const text = getTextContent(lastMessage.content);

  if (toolCalls.length > 0) {
    return {
      label: `${formatRole(lastMessage.role)} tool call`,
      detail: summarizeToolCalls(toolCalls).detail,
    };
  }

  if (toolResults.length > 0) {
    const firstResult = toolResults[0];
    return {
      label: `${firstResult.toolName || 'tool'} result`,
      detail: truncate(formatResultPreview(firstResult.result), 120),
    };
  }

  if (text) {
    return {
      label: formatRole(lastMessage.role),
      detail: truncate(text, 120),
    };
  }

  return {
    label: formatRole(lastMessage.role),
  };
}

export function getStepSummary(
  output: JSONRecord | null,
  error: string | null,
): StepSummaryInfo {
  if (error) {
    return { icon: 'alert', label: 'Error' };
  }

  const toolCalls = output ? getOutputToolCalls(output) : [];
  if (toolCalls.length > 0) {
    const { label, detail } = summarizeToolCalls(toolCalls);
    return { icon: 'wrench', label, detail };
  }

  return { icon: 'message', label: 'Response' };
}

export function summarizeToolCalls(toolCalls: ToolCallPart[]): {
  label: string;
  detail?: string;
} {
  const counts = new Map<string, number>();
  toolCalls.forEach(call => {
    const name = call.toolName || 'tool';
    counts.set(name, (counts.get(name) ?? 0) + 1);
  });
  const entries = Array.from(counts.entries());
  const label = entries
    .slice(0, 2)
    .map(([name]) => name)
    .join(', ');
  const detail = entries
    .slice(0, 3)
    .map(([name, count]) => `${name} × ${count}`)
    .join(' · ');
  return {
    label: label || 'Tool calls',
    detail: detail || undefined,
  };
}

export function getToolResultsFromNextStep(
  steps: ParsedStepRecord[],
  index: number,
): ToolResultPart[] {
  const nextStep = steps[index + 1];
  if (!nextStep) {
    return [];
  }
  return nextStep.parsedInput.prompt
    .filter(message => message.role === 'tool')
    .flatMap(message => getMessageToolResults(message));
}

export function getOutputToolCalls(output: JSONRecord): ToolCallPart[] {
  if (Array.isArray(output.toolCalls)) {
    return output.toolCalls.map(normalizeToolCallPart);
  }
  if (Array.isArray(output.content)) {
    return output.content
      .filter(part => isRecord(part) && part.type === 'tool-call')
      .map(normalizeToolCallPart);
  }
  return [];
}

export function getOutputText(output: JSONRecord): string {
  if (Array.isArray(output.textParts)) {
    return output.textParts
      .map(part => (isRecord(part) && typeof part.text === 'string' ? part.text : ''))
      .join('');
  }
  if (Array.isArray(output.content)) {
    return output.content
      .map(part =>
        isRecord(part) && part.type === 'text' && typeof part.text === 'string'
          ? part.text
          : '',
      )
      .join('');
  }
  return typeof output.text === 'string' ? output.text : '';
}

export function getOutputReasoning(output: JSONRecord): string {
  if (Array.isArray(output.reasoningParts)) {
    return output.reasoningParts
      .map(part =>
        isRecord(part)
          ? typeof part.text === 'string'
            ? part.text
            : typeof part.reasoning === 'string'
              ? part.reasoning
              : ''
          : '',
      )
      .join('');
  }
  if (Array.isArray(output.content)) {
    return output.content
      .map(part =>
        isRecord(part) &&
        (part.type === 'thinking' || part.type === 'reasoning') &&
        typeof (part.text ?? part.reasoning ?? part.thinking) === 'string'
          ? String(part.text ?? part.reasoning ?? part.thinking)
          : '',
      )
      .join('');
  }
  return '';
}

export function getMessageToolCalls(message: PromptMessage): ToolCallPart[] {
  const contentCalls = Array.isArray(message.content)
    ? message.content
        .filter(part => isRecord(part) && part.type === 'tool-call')
        .map(normalizeToolCallPart)
    : [];

  const topLevelCalls = Array.isArray(message.tool_calls)
    ? message.tool_calls.map(call => {
        if (!isRecord(call)) {
          return {};
        }
        const functionValue = isRecord(call.function) ? call.function : {};
        return normalizeToolCallPart({
          toolName: functionValue.name,
          toolCallId: call.id,
          args: functionValue.arguments,
        });
      })
    : [];

  return [...contentCalls, ...topLevelCalls];
}

export function getMessageToolResults(message: PromptMessage): ToolResultPart[] {
  if (Array.isArray(message.content)) {
    return message.content
      .filter(part => isRecord(part) && part.type === 'tool-result')
      .map(part => normalizeToolResultPart(part));
  }

  if (message.role === 'tool' && message.content != null) {
    return [
      {
        toolName: 'tool',
        toolCallId: message.tool_call_id,
        result: message.content,
      },
    ];
  }

  return [];
}

export function normalizeToolCallPart(value: unknown): ToolCallPart {
  if (!isRecord(value)) {
    return {};
  }
  return {
    toolName: typeof value.toolName === 'string' ? value.toolName : undefined,
    toolCallId: typeof value.toolCallId === 'string' ? value.toolCallId : undefined,
    args: value.args ?? value.input,
  };
}

export function normalizeToolResultPart(value: unknown): ToolResultPart {
  if (!isRecord(value)) {
    return {};
  }
  return {
    toolName: typeof value.toolName === 'string' ? value.toolName : undefined,
    toolCallId: typeof value.toolCallId === 'string' ? value.toolCallId : undefined,
    result: value.result ?? value.output ?? value,
  };
}

export function getTextContent(content: unknown): string {
  if (typeof content === 'string') {
    return content;
  }
  if (Array.isArray(content)) {
    return content
      .map(part =>
        isRecord(part) && part.type === 'text' && typeof part.text === 'string'
          ? part.text
          : '',
      )
      .join('');
  }
  return '';
}

export function getReasoningContent(content: unknown): string {
  if (!Array.isArray(content)) {
    return '';
  }
  return content
    .map(part =>
      isRecord(part) &&
      (part.type === 'thinking' || part.type === 'reasoning') &&
      typeof (part.thinking ?? part.text ?? part.reasoning) === 'string'
        ? String(part.thinking ?? part.text ?? part.reasoning)
        : '',
    )
    .join('');
}

export function getInputTokenBreakdown(
  tokens: number | InputTokenBreakdown | null | undefined,
): InputTokenBreakdown {
  if (tokens == null) {
    return { total: 0 };
  }
  if (typeof tokens === 'number') {
    return { total: tokens };
  }
  if (typeof tokens === 'object') {
    return {
      total: typeof tokens.total === 'number' ? tokens.total : 0,
      ...(typeof tokens.noCache === 'number' && { noCache: tokens.noCache }),
      ...(typeof tokens.cacheRead === 'number' && { cacheRead: tokens.cacheRead }),
      ...(typeof tokens.cacheWrite === 'number' && { cacheWrite: tokens.cacheWrite }),
    };
  }
  return { total: 0 };
}

export function getOutputTokenBreakdown(
  tokens: number | OutputTokenBreakdown | null | undefined,
): OutputTokenBreakdown {
  if (tokens == null) {
    return { total: 0 };
  }
  if (typeof tokens === 'number') {
    return { total: tokens };
  }
  if (typeof tokens === 'object') {
    return {
      total: typeof tokens.total === 'number' ? tokens.total : 0,
      ...(typeof tokens.text === 'number' && { text: tokens.text }),
      ...(typeof tokens.reasoning === 'number' && {
        reasoning: tokens.reasoning,
      }),
    };
  }
  return { total: 0 };
}

export function formatInputTokens(breakdown: InputTokenBreakdown): string {
  const { total, cacheRead } = breakdown;
  if (cacheRead && cacheRead > 0) {
    return `${total} (${cacheRead} cached)`;
  }
  return String(total);
}

export function formatOutputTokens(breakdown: OutputTokenBreakdown): string {
  const { total, reasoning } = breakdown;
  if (reasoning && reasoning > 0) {
    return `${total} (${reasoning} reasoning)`;
  }
  return String(total);
}

export function formatToolParams(args: unknown): string {
  if (!isRecord(args)) {
    return '';
  }
  const entries = Object.entries(args);
  if (entries.length === 0) {
    return '';
  }
  const [key, value] = entries[0];
  const preview = formatScalarPreview(value, 20);
  return entries.length === 1 ? `{ ${key}: ${preview} }` : `{ ${key}: ${preview}, … }`;
}

export function formatToolParamsInline(args: unknown): string {
  return formatToolParams(args);
}

export function formatResultPreview(result: unknown): string {
  if (typeof result === 'string') {
    return truncate(JSON.stringify(result), 36);
  }
  if (Array.isArray(result)) {
    return `[${result.length}]`;
  }
  if (isRecord(result)) {
    const entries = Object.entries(result);
    if (entries.length === 0) {
      return '{}';
    }
    const [key, value] = entries[0];
    return entries.length === 1
      ? `{ ${key}: ${formatScalarPreview(value, 15)} }`
      : `{ ${key}: ${formatScalarPreview(value, 15)}, … }`;
  }
  if (result == null) {
    return 'null';
  }
  return String(result);
}

export function formatScalarPreview(value: unknown, maxLength: number): string {
  if (typeof value === 'string') {
    return value.length > maxLength
      ? `"${value.slice(0, maxLength)}…"`
      : JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.length}]`;
  }
  if (isRecord(value)) {
    return '{…}';
  }
  if (value == null) {
    return 'null';
  }
  return String(value);
}

export function truncate(value: string, maxLength: number): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value;
}

export function formatRole(role: string): string {
  switch (role) {
    case 'user':
      return 'User';
    case 'assistant':
      return 'Assistant';
    case 'system':
      return 'System';
    case 'tool':
      return 'Tool';
    default:
      return role;
  }
}

export function firstRunID(runs: RunNode[]): string | null {
  for (const run of runs) {
    if (run.id) {
      return run.id;
    }
  }
  return null;
}

export function flattenRunIDs(runs: RunNode[]): Set<string> {
  const output = new Set<string>();
  const visit = (node: RunNode) => {
    output.add(node.id);
    node.children.forEach(visit);
  };
  runs.forEach(visit);
  return output;
}

export function formatTime(value: string | null | undefined): string {
  if (!value) {
    return 'n/a';
  }
  return new Date(value).toLocaleString();
}

export function formatFinishedAt(value: string | null | undefined): string {
  if (!value) {
    return 'still running';
  }
  return `finished ${new Date(value).toLocaleString()}`;
}

export function formatDuration(durationMS: number | null): string {
  if (durationMS == null) {
    return 'running';
  }
  if (durationMS < 1000) {
    return `${durationMS}ms`;
  }
  return `${(durationMS / 1000).toFixed(2)}s`;
}
