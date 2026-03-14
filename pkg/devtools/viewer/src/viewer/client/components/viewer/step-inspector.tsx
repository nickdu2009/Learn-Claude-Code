import React, { useId, useState } from 'react';
import { BarChart3, ChevronRight, MessageSquare, Settings, Wrench } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from '@/components/ui/drawer';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  formatResultPreview,
  formatRole,
  stripMarkdownForPreview,
  formatToolParams,
  formatToolParamsInline,
  getInputTokenBreakdown,
  getMessageToolCalls,
  getMessageToolResults,
  getOutputReasoning,
  getOutputText,
  getOutputTokenBreakdown,
  getOutputToolCalls,
  getReasoningContent,
  getTextContent,
  isRecord,
} from '@/lib/viewer-helpers';
import type {
  InputTokenBreakdown,
  JSONRecord,
  OutputTokenBreakdown,
  ParsedStepRecord,
  PromptMessage,
  StepInput,
  ToolResultPart,
} from '@/lib/viewer-types';
import {
  JsonBlock,
  MarkdownBlock,
  ReasoningBlock,
  TextBlock,
  TokenBreakdownTooltip,
} from '@/components/viewer/shared';

export function StepConfigBar({
  modelId,
  provider,
  input,
  providerOptions,
  usage,
}: {
  modelId?: string;
  provider?: string | null;
  input: StepInput;
  providerOptions?: JSONRecord | null;
  usage?: JSONRecord | null;
}) {
  const tools = Array.isArray(input.tools) ? input.tools : [];
  const params: Array<{ label: string; value: string }> = [];

  if (input.temperature != null) {
    params.push({ label: 'temp', value: String(input.temperature) });
  }
  if (input.maxOutputTokens != null) {
    params.push({ label: 'max tokens', value: String(input.maxOutputTokens) });
  }
  if (input.topP != null) {
    params.push({ label: 'topP', value: String(input.topP) });
  }
  if (input.topK != null) {
    params.push({ label: 'topK', value: String(input.topK) });
  }
  if (input.toolChoice != null) {
    const choice = isRecord(input.toolChoice)
      ? String(input.toolChoice.type ?? 'custom')
      : String(input.toolChoice);
    params.push({ label: 'tool choice', value: choice });
  }

  return (
    <div className="flex flex-wrap items-center gap-2 border-t border-border bg-muted/20 px-4 py-2 text-[11px] text-muted-foreground">
      {provider && (
        <Badge className="rounded bg-sidebar-primary/10 px-1.5 py-0.5 text-[10px] font-medium text-sidebar-primary">
          {provider}
        </Badge>
      )}
      {modelId && <span className="font-mono">{modelId}</span>}
      {params.map(param => (
        <Badge key={param.label} variant="outline" className="text-[10px]">
          {param.label}: {param.value}
        </Badge>
      ))}
      {tools.length > 0 && (
        <Drawer direction="right">
          <DrawerTrigger asChild>
            <button className="inline-flex cursor-pointer items-center gap-1 transition-colors hover:text-foreground">
              <Wrench className="size-3" />
              {tools.length} available {tools.length === 1 ? 'tool' : 'tools'}
            </button>
          </DrawerTrigger>
          <DrawerContent className="h-full w-[800px] max-w-[90vw] overflow-hidden border-border">
            <DrawerHeader className="shrink-0 border-b border-border">
              <DrawerTitle>Available Tools ({tools.length})</DrawerTitle>
            </DrawerHeader>
            <ScrollArea className="h-full p-4">
              <div className="space-y-3">
                {tools.map((tool, index) => (
                  <ToolDefinitionCard key={index} tool={tool} />
                ))}
              </div>
            </ScrollArea>
          </DrawerContent>
        </Drawer>
      )}
      {providerOptions && Object.keys(providerOptions).length > 0 && (
        <Drawer direction="right">
          <DrawerTrigger asChild>
            <button className="inline-flex cursor-pointer items-center gap-1 transition-colors hover:text-foreground">
              <Settings className="size-3" />
              Provider Options
            </button>
          </DrawerTrigger>
          <DrawerContent className="h-full w-[800px] max-w-[90vw] overflow-hidden border-border">
            <DrawerHeader className="shrink-0 border-b border-border">
              <DrawerTitle>Provider Options</DrawerTitle>
            </DrawerHeader>
            <div className="flex-1 overflow-y-auto p-4">
              <JsonBlock data={providerOptions} compact={false} />
            </div>
          </DrawerContent>
        </Drawer>
      )}
      {usage && (
        <Drawer direction="right">
          <DrawerTrigger asChild>
            <button className="inline-flex cursor-pointer items-center gap-1 transition-colors hover:text-foreground">
              <BarChart3 className="size-3" />
              Usage
            </button>
          </DrawerTrigger>
          <DrawerContent className="h-full w-[800px] max-w-[90vw] overflow-hidden border-border">
            <DrawerHeader className="shrink-0 border-b border-border">
              <DrawerTitle>Token Usage</DrawerTitle>
            </DrawerHeader>
            <ScrollArea className="h-full p-4">
              <UsageDetails usage={usage} />
            </ScrollArea>
          </DrawerContent>
        </Drawer>
      )}
    </div>
  );
}

export function InputPanel({ input }: { input: StepInput }) {
  const messages = input.prompt;
  const lastTwoMessages = messages.slice(Math.max(0, messages.length - 2));
  const previousMessageCount = Math.max(0, messages.length - 2);

  return (
    <Drawer direction="right">
      <DrawerTrigger asChild>
        <div
          role="button"
          tabIndex={0}
          aria-label={`Open all input messages (${messages.length})`}
          className="flex h-full w-full cursor-pointer flex-col justify-start p-4 text-left transition-colors hover:bg-accent/30"
        >
          <div className="mb-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            Input
          </div>

          {previousMessageCount > 0 && (
            <div className="mb-3 rounded-md bg-muted/30 py-1.5 text-center text-[11px] text-muted-foreground/60">
              + {previousMessageCount} previous{' '}
              {previousMessageCount === 1 ? 'message' : 'messages'}
            </div>
          )}

          <div className="flex flex-col gap-3">
            {lastTwoMessages.map((message, index) => (
              <InputMessagePreview
                key={`${message.role}-${index}`}
                message={message}
                index={messages.length - lastTwoMessages.length + index + 1}
              />
            ))}
            {messages.length === 0 && (
              <div className="text-sm text-muted-foreground">No messages</div>
            )}
          </div>
        </div>
      </DrawerTrigger>
      <DrawerContent className="h-full w-[800px] max-w-[90vw] overflow-hidden border-border">
        <DrawerHeader className="shrink-0 border-b border-border">
          <DrawerTitle>All Messages ({messages.length})</DrawerTitle>
        </DrawerHeader>
        <div className="flex-1 overflow-y-auto p-4 pb-8">
          <div className="flex flex-col gap-3">
            {messages.map((message, index) => (
              <MessageBubble
                key={`${message.role}-${index}`}
                message={message}
                index={index + 1}
              />
            ))}
          </div>
        </div>
      </DrawerContent>
    </Drawer>
  );
}

export function InputMessagePreview({
  message,
  index,
}: {
  message: PromptMessage;
  index?: number;
}) {
  const toolCalls = getMessageToolCalls(message);
  const toolResults = getMessageToolResults(message);
  const textContent = getTextContent(message.content);
  const previewText = textContent ? stripMarkdownForPreview(textContent) : '';
  const reasoningContent = getReasoningContent(message.content);
  const partCount =
    (textContent ? 1 : 0) +
    (reasoningContent ? 1 : 0) +
    toolCalls.length +
    toolResults.length;

  return (
    <div className="space-y-2 rounded-md border border-border/50 bg-background/50 p-2.5">
      <div className="flex items-center gap-2">
        {index != null && (
          <span className="text-[10px] font-mono text-muted-foreground/50">
            {index}
          </span>
        )}
        <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          {formatRole(message.role)}
        </span>
        {partCount > 1 && (
          <span className="text-[10px] text-muted-foreground/60">{partCount} parts</span>
        )}
      </div>
      {reasoningContent && (
        <div className="text-xs text-amber-500/60">[thinking]</div>
      )}
      {textContent && (
        <div className="line-clamp-3 text-xs text-foreground/90">{previewText}</div>
      )}
      {toolCalls.length > 0 && (
        <div className="space-y-1">
          {toolCalls.slice(0, 3).map((call, callIndex) => (
            <div
              key={`${call.toolCallId ?? call.toolName ?? 'tool'}-${callIndex}`}
              className="truncate font-mono text-[11px] text-muted-foreground"
            >
              {call.toolName || 'tool'}({formatToolParamsInline(call.args)})
            </div>
          ))}
          {toolCalls.length > 3 && (
            <div className="text-[11px] text-muted-foreground/60">
              +{toolCalls.length - 3} more tool calls
            </div>
          )}
        </div>
      )}
      {toolResults.length > 0 && (
        <div className="space-y-1">
          {toolResults.slice(0, 3).map((result, resultIndex) => (
            <div
              key={`${result.toolCallId ?? result.toolName ?? 'tool'}-${resultIndex}`}
              className="truncate font-mono text-[11px] text-muted-foreground"
            >
              {result.toolName || 'tool'}(…) =&gt; {formatResultPreview(result.result)}
            </div>
          ))}
          {toolResults.length > 3 && (
            <div className="text-[11px] text-muted-foreground/60">
              +{toolResults.length - 3} more tool results
            </div>
          )}
        </div>
      )}
      {!textContent &&
        !reasoningContent &&
        toolCalls.length === 0 &&
        toolResults.length === 0 && (
          <div className="text-[11px] italic text-muted-foreground">Empty message</div>
        )}
    </div>
  );
}

export function MessageBubble({
  message,
  index,
}: {
  message: PromptMessage;
  index?: number;
}) {
  const toolCalls = getMessageToolCalls(message);
  const toolResults = getMessageToolResults(message);
  const textContent = getTextContent(message.content);
  const reasoningContent = getReasoningContent(message.content);
  const isSystem = message.role === 'system';
  const expandSingleToolResultByDefault =
    message.role === 'tool' &&
    !textContent &&
    !reasoningContent &&
    toolCalls.length === 0 &&
    toolResults.length === 1;
  const partCount =
    (textContent ? 1 : 0) +
    (reasoningContent ? 1 : 0) +
    toolCalls.length +
    toolResults.length;

  return (
    <div className="space-y-2 rounded-md border border-border/50 bg-background/50 p-3">
      <div className="flex items-center gap-2">
        {index != null && (
          <span className="text-[10px] font-mono text-muted-foreground/50">{index}</span>
        )}
        <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          {formatRole(message.role)}
        </span>
        {partCount > 1 && (
          <span className="text-[10px] text-muted-foreground/60">{partCount} parts</span>
        )}
      </div>

      <div className="space-y-3">
        {reasoningContent && <ReasoningBlock content={reasoningContent} />}
        {textContent && (
          <TextBlock
            content={textContent}
            defaultExpanded={
              isSystem ||
              (!reasoningContent && toolCalls.length === 0 && toolResults.length === 0)
            }
            isSystem={isSystem}
          />
        )}
        {toolCalls.map((call, index) => (
          <CollapsibleToolCall
            key={`${call.toolCallId ?? call.toolName ?? 'tool'}-${index}`}
            toolName={call.toolName || 'tool'}
            toolCallId={call.toolCallId}
            data={call.args}
          />
        ))}
        {toolResults.map((result, index) => (
          <CollapsibleToolResult
            key={`${result.toolCallId ?? result.toolName ?? 'tool'}-${index}`}
            toolName={result.toolName}
            toolCallId={result.toolCallId}
            data={result.result}
            defaultExpanded={expandSingleToolResultByDefault}
          />
        ))}
        {!textContent &&
          !reasoningContent &&
          toolCalls.length === 0 &&
          toolResults.length === 0 && (
            <div className="text-sm text-muted-foreground">Empty message</div>
          )}
      </div>
    </div>
  );
}

export function OutputDisplay({
  output,
  toolResults = [],
}: {
  output: JSONRecord;
  toolResults?: ToolResultPart[];
}) {
  const toolCalls = getOutputToolCalls(output);
  const textContent = getOutputText(output);
  const reasoningContent = getOutputReasoning(output);

  return (
    <div className="flex flex-col gap-3">
      {reasoningContent && <ReasoningBlock content={reasoningContent} />}
      {textContent && (
        <TextBlock content={textContent} defaultExpanded={!toolCalls.length} />
      )}
      {toolCalls.map((call, index) => {
        const result = toolResults.find(item => item.toolCallId === call.toolCallId);
        return (
          <ToolCallCard
            key={`${call.toolCallId ?? call.toolName ?? 'tool'}-${index}`}
            toolName={call.toolName || 'tool'}
            args={call.args}
            result={result?.result}
          />
        );
      })}
      {!textContent && !reasoningContent && toolCalls.length === 0 && (
        <JsonBlock data={output} compact={false} />
      )}
    </div>
  );
}

export function ToolDefinitionCard({ tool }: { tool: unknown }) {
  const normalized = isRecord(tool) ? tool : { name: 'tool', value: tool };
  const name = typeof normalized.name === 'string' ? normalized.name : 'tool';
  const description =
    typeof normalized.description === 'string' ? normalized.description : null;
  const parameters =
    normalized.parameters != null && isRecord(normalized.parameters)
      ? normalized.parameters
      : null;
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="overflow-hidden rounded-md border border-border bg-background">
      <button
        className="flex w-full items-center justify-between px-2.5 py-2 text-left transition-colors hover:bg-accent/50"
        onClick={() => {
          if (parameters) {
            setExpanded(previous => !previous);
          }
        }}
      >
        <span className="text-xs font-mono text-purple">{name}</span>
        {parameters && (
          <ChevronRight
            className={`size-3 text-muted-foreground transition-transform ${expanded ? 'rotate-90' : ''
              }`}
          />
        )}
      </button>
      {expanded && parameters && (
        <div className="border-t border-border px-2.5 pb-2.5">
          {description && (
            <MarkdownBlock
              content={description}
              className="mb-2 pt-2 text-[11px] leading-relaxed text-muted-foreground"
            />
          )}
          <JsonBlock data={parameters} compact />
        </div>
      )}
      {!expanded && description && (
        <div className="-mt-1 px-2.5 pb-2">
          <p className="truncate text-[11px] text-muted-foreground">
            {stripMarkdownForPreview(description)}
          </p>
        </div>
      )}
    </div>
  );
}

function ToolResultContent({ data }: { data: unknown }) {
  if (typeof data === 'string') {
    return (
      <div className="max-h-72 overflow-y-auto">
        <MarkdownBlock content={data} className="text-xs leading-relaxed text-foreground" />
      </div>
    );
  }

  return <JsonBlock data={data} compact={false} />;
}

export function CollapsibleToolCall({
  toolName,
  toolCallId,
  data,
}: {
  toolName: string;
  toolCallId?: string;
  data: unknown;
}) {
  const [expanded, setExpanded] = useState(false);
  const contentID = useId();

  return (
    <div className="overflow-hidden rounded-md border border-purple/30">
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={contentID}
        className="flex w-full items-center gap-2 bg-purple/10 px-3 py-2 text-left transition-colors hover:bg-purple/20"
        onClick={() => setExpanded(previous => !previous)}
      >
        <ChevronRight
          className={`size-3 shrink-0 text-purple transition-transform ${expanded ? 'rotate-90' : ''
            }`}
        />
        <Wrench className="size-3 shrink-0 text-purple" />
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-xs font-medium text-purple">
            {toolName}
          </span>
          {!expanded && (
            <span className="truncate text-[11px] font-mono text-purple/70">
              {formatToolParams(data)}
            </span>
          )}
        </div>
        {toolCallId && (
          <span className="ml-auto shrink-0 text-[10px] font-mono text-muted-foreground/60">
            {toolCallId}
          </span>
        )}
      </button>
      {expanded && (
        <div id={contentID} className="border-t border-purple/30 bg-card/50 p-3">
          <div className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            Input
          </div>
          <JsonBlock data={data} compact={false} />
        </div>
      )}
    </div>
  );
}

export function CollapsibleToolResult({
  toolName,
  toolCallId,
  data,
  defaultExpanded = false,
}: {
  toolName?: string;
  toolCallId?: string;
  data: unknown;
  defaultExpanded?: boolean;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const contentID = useId();

  return (
    <div className="overflow-hidden rounded-md border border-success/30">
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={contentID}
        className="flex w-full items-center gap-2 bg-success/10 px-3 py-2 text-left transition-colors hover:bg-success/20"
        onClick={() => setExpanded(previous => !previous)}
      >
        <ChevronRight
          className={`size-3 shrink-0 text-success transition-transform ${expanded ? 'rotate-90' : ''
            }`}
        />
        <div className="flex min-w-0 items-center gap-2">
          <span className="text-xs font-medium text-success">Result</span>
          <MessageSquare className="size-3 shrink-0 text-success" />
          <span className="truncate text-[11px] font-mono text-muted-foreground">
            Result {toolName ? `· ${toolName}` : ''}
          </span>
          {!expanded && (
            <span className="truncate text-[11px] text-success/70">
              {formatResultPreview(data)}
            </span>
          )}
        </div>
        {toolCallId && (
          <span className="ml-auto shrink-0 text-[10px] font-mono text-muted-foreground/60">
            {toolCallId}
          </span>
        )}
      </button>
      {expanded && (
        <div id={contentID} className="border-t border-success/30 bg-card/50 p-3">
          <div className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            Output
          </div>
          <ToolResultContent data={data} />
        </div>
      )}
    </div>
  );
}

export function ToolCallCard({
  toolName,
  args,
  result,
}: {
  toolName: string;
  args: unknown;
  result?: unknown;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="overflow-hidden rounded-md border border-purple/30">
      <button
        className="flex w-full items-center gap-2 bg-purple/10 px-3 py-2 text-left transition-colors hover:bg-purple/20"
        onClick={() => setExpanded(previous => !previous)}
      >
        <ChevronRight
          className={`size-3 shrink-0 text-purple transition-transform ${expanded ? 'rotate-90' : ''
            }`}
        />
        <Wrench className="size-3 shrink-0 text-purple" />
        <div className="flex min-w-0 items-center gap-2">
          <span className="text-xs font-medium text-purple">{toolName}</span>
          {!expanded && (
            <span className="truncate text-[11px] font-mono text-purple/70">
              {formatToolParams(args)}
            </span>
          )}
        </div>
      </button>
      {expanded && (
        <>
          <div className="border-t border-purple/30 bg-card/50 p-3">
            <div className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              Input
            </div>
            <JsonBlock data={args} compact={false} />
          </div>
          <div className="mb-3">
            {result !== undefined && (
              <div className="border-t border-border bg-success/5 p-3">
                <div className="mb-2 text-[10px] font-medium uppercase tracking-wider text-success">
                  Output
                </div>
                <ToolResultContent data={result} />
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}

export function RawDataSection({ step }: { step: ParsedStepRecord }) {
  const [expanded, setExpanded] = useState(false);
  const [responseView, setResponseView] = useState<'parsed' | 'raw'>('parsed');

  if (!step.parsedRawRequest && !step.parsedRawResponse && !step.parsedRawChunks) {
    return null;
  }

  const hasProviderStream = step.type === 'stream' && step.parsedRawChunks != null;
  const responseData =
    hasProviderStream && responseView === 'raw'
      ? step.parsedRawChunks
      : step.parsedRawResponse;

  return (
    <div className="border-t border-border">
      <button
        className="flex w-full items-center gap-2 px-4 py-2.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent/30 hover:text-foreground"
        onClick={() => setExpanded(previous => !previous)}
      >
        <ChevronRight
          className={`size-3 transition-transform ${expanded ? 'rotate-90' : ''}`}
        />
        <span className="font-medium uppercase tracking-wider">
          Request / Response
        </span>
      </button>

      {expanded && (
        <div className="grid grid-cols-2 gap-4 px-4 pb-4 pt-1">
          <div className="space-y-2">
            <div className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              Request
            </div>
            <JsonBlock data={step.parsedRawRequest} compact={false} />
          </div>
          <div className="space-y-2">
            <div className="mb-2 flex items-center justify-between">
              <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                {step.type === 'stream' ? 'Stream' : 'Response'}
              </div>
              {hasProviderStream && (
                <div className="flex items-center rounded-md border border-border/50 bg-background/50 p-1 text-[10px]">
                  <button
                    className={`rounded px-2 py-0.5 transition-colors ${responseView === 'parsed'
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                      }`}
                    onClick={() => setResponseView('parsed')}
                  >
                    AI SDK
                  </button>
                  <button
                    className={`rounded px-2 py-0.5 transition-colors ${responseView === 'raw'
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                      }`}
                    onClick={() => setResponseView('raw')}
                  >
                    Provider
                  </button>
                </div>
              )}
            </div>
            <JsonBlock data={responseData} compact={false} />
          </div>
        </div>
      )}
    </div>
  );
}

export function UsageDetails({ usage }: { usage: JSONRecord }) {
  const inputBreakdown = getInputTokenBreakdown(
    usage.inputTokens as number | InputTokenBreakdown | null | undefined,
  );
  const outputBreakdown = getOutputTokenBreakdown(
    usage.outputTokens as number | OutputTokenBreakdown | null | undefined,
  );

  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-2 gap-3">
        <div className="rounded-md border border-border/50 bg-background/50 p-2.5">
          <div className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            Input Tokens
          </div>
          <div className="font-mono text-sm font-semibold text-foreground">
            {inputBreakdown.total}
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
            {inputBreakdown.cacheRead !== undefined && (
              <span>Cache read {inputBreakdown.cacheRead}</span>
            )}
            {inputBreakdown.cacheWrite !== undefined && (
              <span>Cache write {inputBreakdown.cacheWrite}</span>
            )}
            {inputBreakdown.noCache !== undefined && (
              <span>No cache {inputBreakdown.noCache}</span>
            )}
          </div>
        </div>
        <div className="rounded-md border border-border/50 bg-background/50 p-2.5">
          <div className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            Output Tokens
          </div>
          <div className="font-mono text-sm font-semibold text-foreground">
            {outputBreakdown.total}
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
            {outputBreakdown.text !== undefined && (
              <span>Text {outputBreakdown.text}</span>
            )}
            {outputBreakdown.reasoning !== undefined && (
              <span>Reasoning {outputBreakdown.reasoning}</span>
            )}
          </div>
        </div>
      </div>

      {usage.raw !== undefined && (
        <div>
          <div className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
            Raw Provider Usage
          </div>
          <JsonBlock data={usage.raw} compact={false} />
        </div>
      )}

      <div>
        <div className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
          Full Usage Object
        </div>
        <JsonBlock data={usage} compact={false} />
      </div>
    </div>
  );
}

export function TokenTooltipSummary({
  usage,
}: {
  usage: JSONRecord;
}) {
  return (
    <TokenBreakdownTooltip
      input={getInputTokenBreakdown(
        usage.inputTokens as number | InputTokenBreakdown | null | undefined,
      )}
      output={getOutputTokenBreakdown(
        usage.outputTokens as number | OutputTokenBreakdown | null | undefined,
      )}
      raw={usage.raw}
    />
  );
}
