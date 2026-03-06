import React from 'react';
import {
  AlertCircle,
  GitBranch,
  Loader2,
  MessageSquare,
  Wrench,
} from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import {
  formatDuration,
  formatFinishedAt,
  formatInputTokens,
  formatTime,
  formatOutputTokens,
  getFirstUserMessage,
  getInputTokenBreakdown,
  getOutputTokenBreakdown,
  getTotalTokens,
} from '@/lib/viewer-helpers';
import type {
  InputTokenBreakdown,
  OutputTokenBreakdown,
  ParsedStepRecord,
  RunNode,
  RunRecord,
  StepInputSummary,
  StepSummaryInfo,
} from '@/lib/viewer-types';
import { RunStatusBadge, TokenBreakdownTooltip } from '@/components/viewer/shared';

export function RunHeader({
  run,
  parent,
  steps,
  onSelectParent,
}: {
  run: RunRecord;
  parent?: RunRecord | null;
  steps: ParsedStepRecord[];
  onSelectParent: (runID: string) => void;
}) {
  const firstMessage = getFirstUserMessage(steps);
  const totalDuration = steps.reduce((sum, step) => sum + (step.duration_ms ?? 0), 0);
  const totalTokens = getTotalTokens(steps);
  const headerSummary = run.summary || run.input_preview || firstMessage;

  return (
    <div className="mb-4">
      <div className="mb-2.5 flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <h2 className="truncate text-[13px] font-medium text-foreground">{run.title}</h2>
          <RunStatusBadge run={run} compact />
        </div>
        <Badge
          variant="secondary"
          className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
            run.kind === 'subagent'
              ? 'bg-blue-500/15 text-blue-400'
              : 'bg-emerald-500/15 text-emerald-400'
          }`}
        >
          {run.kind}
        </Badge>
      </div>

      <div className="flex flex-wrap items-center text-[11px] text-muted-foreground">
        <span>{steps.length} steps</span>
        <span className="px-3 text-muted-foreground/30">·</span>
        <span className="font-mono">{formatDuration(totalDuration)}</span>
        <span className="px-3 text-muted-foreground/30">·</span>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="cursor-help font-mono">
              input: {formatInputTokens(totalTokens.input)} → output:{' '}
              {formatOutputTokens(totalTokens.output)}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            <TokenBreakdownTooltip input={totalTokens.input} output={totalTokens.output} />
          </TooltipContent>
        </Tooltip>
        <span className="px-3 text-muted-foreground/30">·</span>
        <span>{formatTime(run.started_at)}</span>
        <span className="px-3 text-muted-foreground/30">·</span>
        <span>{formatFinishedAt(run.finished_at)}</span>
      </div>

      {headerSummary && (
        <p className="mt-2.5 text-[13px] leading-relaxed text-muted-foreground">
          {headerSummary}
        </p>
      )}

      {parent && (
        <button
          className="mt-2.5 inline-flex items-center gap-1 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
          onClick={() => onSelectParent(parent.id)}
        >
          <GitBranch className="size-3.5" />
          parent: {parent.title}
        </button>
      )}
    </div>
  );
}

export function RunTreeItem({
  node,
  depth,
  selectedRunID,
  onSelect,
}: {
  node: RunNode;
  depth: number;
  selectedRunID: string | null;
  onSelect: (runID: string) => void;
}) {
  const isSelected = node.id === selectedRunID;
  const leftPadding = 16 + depth * 10;
  const preview = node.summary || node.input_preview || 'No summary yet.';

  const statusIcon =
    node.status === 'running' ? (
      <Loader2 className="mt-0.5 size-3.5 shrink-0 animate-spin text-blue-400" />
    ) : node.status === 'error' ? (
      <AlertCircle className="mt-0.5 size-3.5 shrink-0 text-destructive-foreground" />
    ) : (
      <MessageSquare className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
    );

  return (
    <div>
      <button
        className={`relative w-full overflow-hidden border-b border-border/50 px-4 py-3 text-left transition-colors ${
          isSelected ? 'bg-accent' : 'hover:bg-accent/50'
        }`}
        style={{ paddingLeft: `${leftPadding}px` }}
        onClick={() => onSelect(node.id)}
      >
        {isSelected && <span className="absolute inset-y-0 left-0 w-px bg-sidebar-primary/50" />}
        <div className="min-w-0 overflow-hidden">
          <div className="mb-1.5 flex min-w-0 items-start gap-2 overflow-hidden">
            {statusIcon}
            <span className="flex-1 truncate text-[13px] leading-tight text-foreground">
              {node.title}
            </span>
          </div>
          <div className="ml-[22px] flex min-w-0 flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
            <Badge
              variant="secondary"
              className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
                node.kind === 'subagent'
                  ? 'bg-blue-500/15 text-blue-400'
                  : 'bg-emerald-500/15 text-emerald-400'
              }`}
            >
              {node.kind}
            </Badge>
            <span>
              {node.step_count} {node.step_count === 1 ? 'step' : 'steps'}
            </span>
            <span>·</span>
            <span className="font-mono">{formatTime(node.started_at)}</span>
            {node.child_count > 0 && (
              <>
                <span>·</span>
                <span>
                  {node.child_count} {node.child_count === 1 ? 'child' : 'children'}
                </span>
              </>
            )}
          </div>
          <div className="mt-1 ml-[22px] truncate text-[11px] text-muted-foreground/80">
            {preview}
          </div>
        </div>
      </button>
      {node.children.map(child => (
        <RunTreeItem
          key={child.id}
          node={child}
          depth={depth + 1}
          selectedRunID={selectedRunID}
          onSelect={onSelect}
        />
      ))}
    </div>
  );
}

export function StepSummary({
  inputSummary,
  summary,
  step,
}: {
  inputSummary: StepInputSummary | null;
  summary: StepSummaryInfo;
  step: ParsedStepRecord;
}) {
  const Icon =
    summary.icon === 'alert'
      ? AlertCircle
      : summary.icon === 'wrench'
        ? Wrench
        : MessageSquare;

  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-1.5">
        {inputSummary && (
          <>
            <span className="truncate text-sm font-medium text-foreground">
              {inputSummary.label}
              {inputSummary.detail ? `: ${inputSummary.detail}` : ''}
            </span>
            <span className="shrink-0 text-muted-foreground/50">→</span>
          </>
        )}
        <Icon
          className={`size-3.5 shrink-0 ${
            summary.icon === 'alert' ? 'text-destructive-foreground' : 'text-muted-foreground'
          }`}
        />
        {summary.detail ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="truncate text-sm font-medium text-foreground">
                {summary.label}
              </span>
            </TooltipTrigger>
            <TooltipContent>{summary.detail}</TooltipContent>
          </Tooltip>
        ) : (
          <span className="truncate text-sm font-medium text-foreground">
            {summary.label}
          </span>
        )}
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
        <span>{step.type}</span>
        <span>·</span>
        <span className="font-mono">{step.model_id}</span>
        {step.provider && (
          <>
            <span>·</span>
            <span>{step.provider}</span>
          </>
        )}
      </div>
    </div>
  );
}

export function StepMetadata({ step }: { step: ParsedStepRecord }) {
  return (
    <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
      <Badge variant="outline" className="font-mono text-[10px]">
        {step.type}
      </Badge>
      <span className="font-mono">{step.model_id}</span>
      {step.provider && <span>{step.provider}</span>}
      <span>·</span>
      <span>{formatTime(step.started_at)}</span>
      <span>·</span>
      <span>{formatDuration(step.duration_ms)}</span>
    </div>
  );
}

export function UsageTooltipContent({
  usage,
}: {
  usage: Record<string, unknown>;
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
