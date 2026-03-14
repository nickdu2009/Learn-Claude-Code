import React from 'react';
import {
  AlertCircle,
  ChevronRight,
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
  stripMarkdownForPreview,
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
import {
  CollapsibleMarkdownBlock,
  MarkdownBlock,
  RunStatusBadge,
  TokenBreakdownTooltip,
} from '@/components/viewer/shared';

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
          className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${run.kind === 'subagent'
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
        <CollapsibleMarkdownBlock
          content={headerSummary}
          className="mt-2.5 text-[13px] leading-relaxed text-muted-foreground"
          collapsedHeightClass="max-h-72"
        />
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
  collapsedRunIDs,
  onSelect,
  onToggleCollapse,
}: {
  node: RunNode;
  depth: number;
  selectedRunID: string | null;
  collapsedRunIDs: Set<string>;
  onSelect: (runID: string) => void;
  onToggleCollapse: (runID: string) => void;
}) {
  const isSelected = node.id === selectedRunID;
  const hasChildren = node.children.length > 0;
  const isCollapsed = collapsedRunIDs.has(node.id);
  const [isHovered, setIsHovered] = React.useState(false);
  const preview = node.summary || node.input_preview || 'No summary yet.';
  const previewText = stripMarkdownForPreview(preview);
  const treeOffset = depth > 0 ? depth * 18 : 0;

  const statusIcon =
    node.status === 'running' ? (
      <Loader2 className="mt-1 size-3.5 shrink-0 animate-spin text-blue-400" />
    ) : node.status === 'error' ? (
      <AlertCircle className="mt-1 size-3.5 shrink-0 text-destructive-foreground" />
    ) : (
      <span className="mt-1.5 size-2 shrink-0 rounded-full bg-emerald-400/90" />
    );

  return (
    <div className="space-y-2">
      <div className="relative" style={treeOffset > 0 ? { marginLeft: `${treeOffset}px` } : undefined}>
        {depth > 0 && (
          <>
            <span className="absolute -left-3 top-7 h-px w-3 bg-border/70" />
            <span className="absolute -left-3 -top-3 h-10 w-px bg-border/70" />
          </>
        )}

        {hasChildren ? (
          <button
            type="button"
            aria-label={`${isCollapsed ? 'Expand' : 'Collapse'} child runs for ${node.title}`}
            aria-expanded={!isCollapsed}
            className="absolute left-3 top-3 z-10 inline-flex size-7 items-center justify-center rounded-md border border-border/80 bg-background/40 text-muted-foreground transition-colors hover:border-border hover:bg-accent/60 hover:text-foreground"
            onClick={event => {
              event.stopPropagation();
              onToggleCollapse(node.id);
            }}
          >
            <ChevronRight
              className={`size-3.5 transition-transform ${isCollapsed ? '' : 'rotate-90'}`}
            />
          </button>
        ) : (
          <span
            aria-hidden="true"
            className="absolute left-3 top-3 z-10 inline-flex size-7 items-center justify-center"
          />
        )}

        <button
          className={`relative w-full rounded-xl border px-4 py-3 pl-12 text-left transition-all ${isSelected
            ? 'border-sidebar-primary/50 bg-accent shadow-[inset_0_0_0_1px_rgba(99,102,241,0.08)]'
            : 'border-border/70 bg-background/35 hover:border-border hover:bg-accent/40'
            }`}
          onClick={() => onSelect(node.id)}
          onMouseEnter={() => setIsHovered(true)}
          onMouseLeave={() => setIsHovered(false)}
        >
          {isSelected && (
            <span className="absolute inset-y-0 left-0 w-1 rounded-l-xl bg-sidebar-primary/80" />
          )}
          <div className="min-w-0">
            <div className="mb-1.5 flex min-w-0 items-start gap-2">
              {statusIcon}
              <div className="flex min-w-0 flex-1 items-start gap-2">
                {hasChildren && (
                  <span className="shrink-0 font-mono text-[11px] font-semibold text-muted-foreground/75">
                    +{node.child_count}
                  </span>
                )}
                <span className="min-w-0 whitespace-normal break-all text-[13px] leading-5 font-medium text-foreground">
                  {node.title}
                </span>
              </div>
            </div>
            <div className="ml-[18px] mt-0.5 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1.5 text-[11px] text-muted-foreground">
              <Badge
                variant="secondary"
                className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${node.kind === 'subagent'
                  ? 'bg-blue-500/15 text-blue-400'
                  : 'bg-emerald-500/15 text-emerald-400'
                  }`}
              >
                {node.kind}
              </Badge>
              <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1.5">
                <span className="whitespace-nowrap">
                  {node.step_count} {node.step_count === 1 ? 'step' : 'steps'}
                </span>
                <span className="font-mono whitespace-nowrap text-muted-foreground/85">
                  {formatTime(node.started_at)}
                </span>
              </div>
            </div>
            <div
              className="mt-1 ml-[18px] whitespace-normal break-all text-[11px] leading-relaxed text-muted-foreground/80"
              style={
                isHovered
                  ? undefined
                  : {
                    display: '-webkit-box',
                    WebkitBoxOrient: 'vertical',
                    WebkitLineClamp: 2,
                    overflow: 'hidden',
                  }
              }
            >
              {previewText}
            </div>
          </div>
        </button>
      </div>

      {!isCollapsed &&
        node.children.map(child => (
          <RunTreeItem
            key={child.id}
            node={child}
            depth={depth + 1}
            selectedRunID={selectedRunID}
            collapsedRunIDs={collapsedRunIDs}
            onSelect={onSelect}
            onToggleCollapse={onToggleCollapse}
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
          className={`size-3.5 shrink-0 ${summary.icon === 'alert' ? 'text-destructive-foreground' : 'text-muted-foreground'
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
