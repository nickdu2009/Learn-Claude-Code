import React, { useMemo, useState } from 'react';
import { Brain, Check, ChevronRight, Copy, Loader2, MessageSquare } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import {
  formatInputTokens,
  formatOutputTokens,
  getInputTokenBreakdown,
  getOutputTokenBreakdown,
} from '@/lib/viewer-helpers';
import type {
  InputTokenBreakdown,
  JSONRecord,
  OutputTokenBreakdown,
  RunRecord,
} from '@/lib/viewer-types';

export function JsonBlock({
  data,
  compact = true,
}: {
  data: unknown;
  compact?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  const jsonString = useMemo(() => JSON.stringify(data, null, 2), [data]);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(jsonString);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="group relative">
      <button
        onClick={handleCopy}
        className="absolute right-2 top-2 rounded-md border border-border bg-background p-1.5 opacity-0 transition-opacity group-hover:opacity-100"
        title="Copy to clipboard"
      >
        {copied ? (
          <span className="text-[10px] text-success">copied</span>
        ) : (
          <Copy className="size-3 text-muted-foreground" />
        )}
      </button>
      <pre
        className={`rounded-md border border-border bg-background p-3 text-xs text-muted-foreground whitespace-pre-wrap ${compact ? 'max-h-72 overflow-auto' : ''
          }`}
      >
        {jsonString}
      </pre>
    </div>
  );
}

export function RunStatusBadge({
  run,
  compact = false,
}: {
  run: Pick<RunRecord, 'status' | 'completion_reason'>;
  compact?: boolean;
}) {
  if (run.status === 'running') {
    return (
      <Badge
        variant="secondary"
        className={`gap-1 ${compact ? 'text-[10px]' : 'text-[11px]'} bg-blue-500/15 text-blue-400`}
      >
        <Loader2 className="size-3 animate-spin" />
        running
      </Badge>
    );
  }
  if (run.status === 'error') {
    return (
      <Badge
        variant="destructive"
        className={`${compact ? 'text-[10px]' : 'text-[11px]'}`}
      >
        error
      </Badge>
    );
  }

  const suffix =
    run.completion_reason && run.completion_reason !== 'normal'
      ? ` · ${run.completion_reason}`
      : '';

  return (
    <Badge
      variant="secondary"
      className={`${compact ? 'text-[10px]' : 'text-[11px]'} bg-emerald-500/15 text-emerald-400`}
    >
      completed{suffix}
    </Badge>
  );
}

export function ReasoningBlock({ content }: { content: string }) {
  const [expanded, setExpanded] = useState(false);
  const preview = content.length > 200 ? `${content.slice(0, 200)}…` : content;

  return (
    <div className="overflow-hidden rounded-md border border-amber-500/30">
      <button
        className="flex w-full items-center gap-2 bg-amber-500/10 px-3 py-2 transition-colors hover:bg-amber-500/20"
        onClick={() => setExpanded(previous => !previous)}
      >
        <ChevronRight
          className={`size-3 shrink-0 text-amber-500 transition-transform ${expanded ? 'rotate-90' : ''
            }`}
        />
        <Brain className="size-3 shrink-0 text-amber-500" />
        <span className="text-xs font-medium text-amber-500">Thinking</span>
        {!expanded && (
          <span className="ml-1 truncate text-[11px] text-amber-500/70">
            {preview}
          </span>
        )}
      </button>

      {expanded && (
        <div className="border-t border-amber-500/30 bg-card/50 p-3">
          <div className="whitespace-pre-wrap text-xs leading-relaxed text-foreground/80">
            {content}
          </div>
        </div>
      )}
    </div>
  );
}

export function TextBlock({
  content,
  defaultExpanded = false,
  isSystem = false,
}: {
  content: string;
  defaultExpanded?: boolean;
  isSystem?: boolean;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [copied, setCopied] = useState(false);
  const preview = content.length > 200 ? `${content.slice(0, 200)}…` : content;
  const toggleExpanded = () => setExpanded(previous => !previous);

  const handleCopy = async (event: React.MouseEvent) => {
    event.stopPropagation();
    await navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className={`overflow-hidden rounded-md border ${isSystem ? 'border-blue-500/30' : 'border-border'}`}>
      <button
        className={`flex w-full items-center gap-2 px-3 py-2 transition-colors ${isSystem
            ? 'bg-blue-500/10 hover:bg-blue-500/20'
            : 'bg-muted/30 hover:bg-muted/50'
          }`}
        onClick={toggleExpanded}
        onKeyDown={event => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            toggleExpanded();
          }
        }}
      >
        <ChevronRight
          className={`size-3 shrink-0 transition-transform ${isSystem ? 'text-blue-400' : 'text-muted-foreground'
            } ${expanded ? 'rotate-90' : ''}`}
        />
        <MessageSquare
          className={`size-3 shrink-0 ${isSystem ? 'text-blue-400' : 'text-muted-foreground'
            }`}
        />
        <span
          className={`text-xs font-medium ${isSystem ? 'text-blue-400' : 'text-foreground'
            }`}
        >
          Text
        </span>
        {!expanded && (
          <span
            className={`ml-1 truncate text-[11px] ${isSystem ? 'text-blue-400/70' : 'text-muted-foreground'
              }`}
          >
            {preview}
          </span>
        )}
      </button>

      {expanded && (
        <div className={`group relative border-t bg-card/50 p-3 ${isSystem ? 'border-blue-500/30' : 'border-border'}`}>
          <button
            onClick={handleCopy}
            className="absolute right-1.5 top-1.5 z-10 rounded-md border border-border bg-background p-1.5 opacity-0 transition-opacity group-hover:opacity-100"
            title="Copy to clipboard"
          >
            {copied ? (
              <Check className="size-3 text-success" />
            ) : (
              <Copy className="size-3 text-muted-foreground" />
            )}
          </button>
          <div className="max-h-60 overflow-y-auto whitespace-pre-wrap text-xs leading-relaxed text-foreground">
            {content}
          </div>
        </div>
      )}
    </div>
  );
}

export function TokenBreakdownTooltip({
  input,
  output,
  raw,
}: {
  input: InputTokenBreakdown;
  output: OutputTokenBreakdown;
  raw?: unknown;
}) {
  return (
    <div className="space-y-1 text-xs">
      <div>Input: {input.total}</div>
      {input.cacheRead !== undefined && <div>Cache read: {input.cacheRead}</div>}
      {input.cacheWrite !== undefined && <div>Cache write: {input.cacheWrite}</div>}
      {input.noCache !== undefined && <div>No cache: {input.noCache}</div>}
      <div>Output: {output.total}</div>
      {output.text !== undefined && <div>Text: {output.text}</div>}
      {output.reasoning !== undefined && <div>Reasoning: {output.reasoning}</div>}
      {raw !== undefined && <div>Raw: {JSON.stringify(raw)}</div>}
    </div>
  );
}

export function UsageSummary({
  usage,
}: {
  usage: JSONRecord;
}) {
  return (
    <>
      {formatInputTokens(
        getInputTokenBreakdown(
          usage.inputTokens as number | InputTokenBreakdown | null | undefined,
        ),
      )}{' '}
      →{' '}
      {formatOutputTokens(
        getOutputTokenBreakdown(
          usage.outputTokens as number | OutputTokenBreakdown | null | undefined,
        ),
      )}
    </>
  );
}

export function ExpandChevron({ expanded }: { expanded: boolean }) {
  return (
    <ChevronRight
      className={`size-4 text-muted-foreground transition-transform ${expanded ? 'rotate-90' : ''
        }`}
    />
  );
}
