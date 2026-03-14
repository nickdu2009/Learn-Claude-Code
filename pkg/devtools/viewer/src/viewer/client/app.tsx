import React, { useEffect, useRef, useState } from 'react';
import {
  AlertCircle,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  GripVertical,
  RefreshCw,
  Trash2,
} from 'lucide-react';

import { AISDKLogo } from '@/components/icons';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { RunHeader, RunTreeItem, StepSummary } from '@/components/viewer/run-tree';
import {
  InputPanel,
  OutputDisplay,
  RawDataSection,
  StepConfigBar,
  UsageDetails,
} from '@/components/viewer/step-inspector';
import { RunStatusBadge, TokenBreakdownTooltip } from '@/components/viewer/shared';
import {
  firstRunID,
  flattenRunIDs,
  formatDuration,
  getInputTokenBreakdown,
  getOutputTokenBreakdown,
  getStepInputSummary,
  getStepSummary,
  getToolResultsFromNextStep,
  normalizeRunDetail,
  stripMarkdownForPreview,
  validateRunTree,
} from '@/lib/viewer-helpers';
import type {
  InputTokenBreakdown,
  OutputTokenBreakdown,
  ParsedRunDetail,
  RunDetail,
  RunNode,
  TraceMeta,
  UnsupportedState,
} from '@/lib/viewer-types';

const SIDEBAR_DEFAULT_WIDTH = 340;
const SIDEBAR_MIN_WIDTH = 260;
const SIDEBAR_MAX_WIDTH = 520;
const SIDEBAR_COLLAPSED_WIDTH = 56;

function App() {
  const [meta, setMeta] = useState<TraceMeta | null>(null);
  const [runs, setRuns] = useState<RunNode[]>([]);
  const [selectedRun, setSelectedRun] = useState<ParsedRunDetail | null>(null);
  const [selectedRunID, setSelectedRunID] = useState<string | null>(null);
  const [collapsedRunIDs, setCollapsedRunIDs] = useState<Set<string>>(new Set());
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);
  const [isSidebarResizing, setIsSidebarResizing] = useState(false);
  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR_DEFAULT_WIDTH);
  const [expandedSteps, setExpandedSteps] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [unsupported, setUnsupported] = useState<UnsupportedState | null>(null);
  const sidebarRef = useRef<HTMLElement | null>(null);
  const resizeStartRef = useRef<{ startX: number; startWidth: number } | null>(null);
  const liveSidebarWidthRef = useRef(SIDEBAR_DEFAULT_WIDTH);
  const resizeFrameRef = useRef<number | null>(null);

  const setViewerError = (error: unknown) => {
    setUnsupported({
      title: 'Viewer Error',
      message: error instanceof Error ? error.message : 'unknown error',
    });
  };

  const loadTraceMeta = async (): Promise<TraceMeta | null> => {
    const response = await fetch('/api/trace/meta');
    if (!response.ok) {
      throw new Error(`failed to load trace metadata: ${response.status}`);
    }
    const nextMeta = (await response.json()) as TraceMeta;
    setMeta(nextMeta);
    if (!nextMeta.supported) {
      setUnsupported({
        title: 'Unsupported Trace',
        message:
          nextMeta.message ||
          `Viewer requires Trace V2, but received version ${nextMeta.version}.`,
      });
      return null;
    }
    setUnsupported(null);
    return nextMeta;
  };

  const selectRun = async (runID: string, resetExpandedSteps: boolean = true) => {
    const response = await fetch(`/api/runs/${encodeURIComponent(runID)}`);
    if (!response.ok) {
      throw new Error(`failed to load run ${runID}: ${response.status}`);
    }
    const detail = normalizeRunDetail((await response.json()) as RunDetail);
    setSelectedRun(detail);
    setSelectedRunID(runID);
    if (resetExpandedSteps) {
      setExpandedSteps(new Set());
    }
  };

  const loadRuns = async () => {
    try {
      setLoading(true);
      const nextMeta = await loadTraceMeta();
      if (!nextMeta || !nextMeta.supported) {
        setRuns([]);
        setSelectedRun(null);
        return;
      }

      const response = await fetch('/api/runs');
      if (!response.ok) {
        throw new Error(`failed to load runs: ${response.status}`);
      }
      const nextRuns = (await response.json()) as RunNode[];
      validateRunTree(nextRuns);
      setRuns(nextRuns);

      const nextVisibleRunIDs = flattenRunIDs(nextRuns);
      const preferredRunID =
        selectedRunID && nextVisibleRunIDs.has(selectedRunID)
          ? selectedRunID
          : firstRunID(nextRuns);

      if (preferredRunID) {
        await selectRun(preferredRunID, false);
      } else {
        setSelectedRunID(null);
        setSelectedRun(null);
      }
    } catch (error) {
      setViewerError(error);
      setRuns([]);
      setSelectedRun(null);
    } finally {
      setLoading(false);
    }
  };

  const handleClear = async () => {
    try {
      const response = await fetch('/api/clear', { method: 'POST' });
      if (!response.ok) {
        throw new Error(`failed to clear trace data: ${response.status}`);
      }
      setSelectedRunID(null);
      setSelectedRun(null);
      setExpandedSteps(new Set());
      await loadRuns();
    } catch (error) {
      setViewerError(error);
    }
  };

  useEffect(() => {
    loadRuns().catch(console.error);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const source = new EventSource('/api/events');
    source.addEventListener('trace', () => {
      loadRuns().catch(console.error);
    });
    source.addEventListener('ready', () => { });
    source.onerror = () => {
      source.close();
      setTimeout(() => {
        loadRuns().catch(console.error);
      }, 1500);
    };
    return () => {
      source.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRunID]);

  useEffect(() => {
    if (!selectedRunID) {
      return;
    }

    const ancestorRunIDs = findAncestorRunIDs(runs, selectedRunID);
    if (!ancestorRunIDs || ancestorRunIDs.length === 0) {
      return;
    }

    setCollapsedRunIDs(previous => {
      let changed = false;
      const next = new Set(previous);
      for (const ancestorID of ancestorRunIDs) {
        if (next.delete(ancestorID)) {
          changed = true;
        }
      }
      return changed ? next : previous;
    });
  }, [runs, selectedRunID]);

  useEffect(() => {
    if (!isSidebarResizing) {
      return;
    }

    const handleMouseMove = (event: MouseEvent) => {
      if (!resizeStartRef.current) {
        return;
      }

      const nextWidth = clampSidebarWidth(
        resizeStartRef.current.startWidth + (event.clientX - resizeStartRef.current.startX),
      );
      liveSidebarWidthRef.current = nextWidth;

      if (resizeFrameRef.current !== null) {
        return;
      }

      resizeFrameRef.current = window.requestAnimationFrame(() => {
        if (sidebarRef.current && !isSidebarCollapsed) {
          sidebarRef.current.style.width = `${liveSidebarWidthRef.current}px`;
        }
        resizeFrameRef.current = null;
      });
    };

    const handleMouseUp = () => {
      if (resizeFrameRef.current !== null) {
        window.cancelAnimationFrame(resizeFrameRef.current);
        resizeFrameRef.current = null;
      }
      setSidebarWidth(liveSidebarWidthRef.current);
      setIsSidebarResizing(false);
      resizeStartRef.current = null;
    };

    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    window.addEventListener('mousemove', handleMouseMove);
    window.addEventListener('mouseup', handleMouseUp);

    return () => {
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      if (resizeFrameRef.current !== null) {
        window.cancelAnimationFrame(resizeFrameRef.current);
        resizeFrameRef.current = null;
      }
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
    };
  }, [isSidebarCollapsed, isSidebarResizing]);

  useEffect(() => {
    liveSidebarWidthRef.current = sidebarWidth;
    if (sidebarRef.current) {
      sidebarRef.current.style.width = `${isSidebarCollapsed ? SIDEBAR_COLLAPSED_WIDTH : sidebarWidth}px`;
    }
  }, [isSidebarCollapsed, sidebarWidth]);

  const toggleStep = (stepID: string) => {
    setExpandedSteps(previous => {
      const next = new Set(previous);
      if (next.has(stepID)) {
        next.delete(stepID);
      } else {
        next.add(stepID);
      }
      return next;
    });
  };

  const toggleRunCollapse = (runID: string) => {
    setCollapsedRunIDs(previous => {
      const next = new Set(previous);
      if (next.has(runID)) {
        next.delete(runID);
      } else {
        next.add(runID);
      }
      return next;
    });
  };

  const visibleRunCount = flattenRunIDs(runs).size;
  const sidebarPanelWidth = isSidebarCollapsed ? SIDEBAR_COLLAPSED_WIDTH : sidebarWidth;

  const handleSidebarResizeStart = (event: React.MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
    if (isSidebarCollapsed) {
      return;
    }

    resizeStartRef.current = { startX: event.clientX, startWidth: sidebarWidth };
    setIsSidebarResizing(true);
  };

  if (unsupported) {
    return (
      <div className="min-h-screen bg-background text-foreground">
        <div className="mx-auto flex min-h-screen max-w-2xl items-center px-6 py-12">
          <Card className="w-full border-border/50 bg-card/50 py-0">
            <div className="border-b border-border px-6 py-4.5">
              <div className="mb-2.5 flex items-center gap-2">
                <AlertCircle className="size-4 text-destructive-foreground" />
                <h1 className="text-sm font-medium">{unsupported.title}</h1>
              </div>
              <p className="text-[13px] leading-relaxed text-muted-foreground">
                {unsupported.message}
              </p>
            </div>
            <div className="px-6 py-4 text-[13px] leading-relaxed text-muted-foreground">
              This viewer only renders strict Trace V2 data and refuses to infer
              missing fields or malformed JSON.
            </div>
          </Card>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <header className="flex items-center justify-between border-b border-border bg-card px-5 py-3">
        <div className="flex items-center gap-2">
          <AISDKLogo />
          <span className="text-base font-medium text-muted-foreground">
            DevTools
          </span>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">
            {meta?.generated_at
              ? `updated ${new Date(meta.generated_at).toLocaleString()}`
              : 'waiting for trace data'}
          </span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => loadRuns().catch(console.error)}
            className="h-8 px-3 text-xs"
          >
            <RefreshCw className="size-3.5" />
            Refresh
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => handleClear().catch(console.error)}
            className="h-8 px-3 text-xs text-destructive-foreground hover:bg-destructive/20"
          >
            <Trash2 className="size-3.5" />
            Clear
          </Button>
        </div>
      </header>

      <div className="flex min-w-0 flex-1 overflow-hidden">
        <aside
          ref={sidebarRef}
          className={`flex min-h-0 shrink-0 flex-col border-r border-border bg-sidebar ${isSidebarResizing ? '' : 'transition-[width] duration-200 ease-out'
            }`}
          style={{ width: `${sidebarPanelWidth}px` }}
        >
          <div className={`border-b border-border bg-sidebar ${isSidebarCollapsed ? 'px-2 py-3' : 'px-3 py-3'}`}>
            {isSidebarCollapsed ? (
              <div className="flex justify-center">
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label="Expand runs sidebar"
                  onClick={() => setIsSidebarCollapsed(false)}
                  className="size-8 p-0 text-muted-foreground hover:text-foreground"
                >
                  <ChevronRight className="size-4" />
                </Button>
              </div>
            ) : (
              <div className="rounded-xl border border-border/80 bg-background/20 px-4 py-3 shadow-sm">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">
                      Runs
                    </div>
                    <div className="mt-2 flex items-center justify-between gap-3">
                      <div className="min-w-0 text-[15px] font-semibold text-foreground">
                        Execution tree
                      </div>
                      <span className="inline-flex min-w-9 items-center justify-center rounded-full border border-sidebar-primary/30 bg-sidebar-primary/10 px-2 py-0.5 font-mono text-[11px] text-sidebar-primary">
                        {visibleRunCount}
                      </span>
                    </div>
                    <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
                      Pinned navigation for runs, subagents, and handoffs.
                    </p>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label="Collapse runs sidebar"
                    onClick={() => setIsSidebarCollapsed(true)}
                    className="mt-0.5 size-8 shrink-0 p-0 text-muted-foreground hover:text-foreground"
                  >
                    <ChevronLeft className="size-4" />
                  </Button>
                </div>
              </div>
            )}
          </div>
          {!isSidebarCollapsed && (
            <ScrollArea className="min-h-0 flex-1 overflow-hidden">
              {loading ? (
                <p className="p-4 text-sm text-muted-foreground">Loading...</p>
              ) : runs.length === 0 ? (
                <p className="p-4 text-sm text-muted-foreground">No runs yet</p>
              ) : (
                <div className="space-y-2 px-3 py-2">
                  {runs.map(node => (
                    <RunTreeItem
                      key={node.id}
                      node={node}
                      depth={0}
                      selectedRunID={selectedRunID}
                      collapsedRunIDs={collapsedRunIDs}
                      onSelect={runID => {
                        selectRun(runID).catch(setViewerError);
                      }}
                      onToggleCollapse={toggleRunCollapse}
                    />
                  ))}
                </div>
              )}
            </ScrollArea>
          )}
        </aside>

        {!isSidebarCollapsed && (
          <div className="relative shrink-0 border-r border-border/60 bg-background/20">
            <button
              type="button"
              aria-label="Resize runs sidebar"
              className={`flex h-full w-3 items-center justify-center text-muted-foreground transition-colors hover:bg-accent/40 hover:text-foreground ${isSidebarResizing ? 'bg-accent/50 text-foreground' : ''
                }`}
              onMouseDown={handleSidebarResizeStart}
              onDoubleClick={() => setSidebarWidth(SIDEBAR_DEFAULT_WIDTH)}
            >
              <GripVertical className="size-3.5" />
            </button>
          </div>
        )}

        <main className="min-h-0 min-w-0 flex-1 overflow-hidden">
          <ScrollArea className="h-full">
            {!selectedRun ? (
              <div className="flex min-h-[calc(100vh-57px)] items-center justify-center text-muted-foreground">
                <p className="text-[13px]">Select a run to view details</p>
              </div>
            ) : (
              <div className="min-w-0 p-5">
                <RunHeader
                  run={selectedRun.run}
                  parent={selectedRun.parent}
                  steps={selectedRun.steps}
                  onSelectParent={runID => {
                    selectRun(runID).catch(setViewerError);
                  }}
                />

                <div className="flex flex-col gap-3">
                  {selectedRun.steps.map((step, index) => {
                    const isExpanded = expandedSteps.has(step.id);
                    const isLastStep = index === selectedRun.steps.length - 1;
                    const isActiveStep =
                      isLastStep && selectedRun.run.status === 'running';
                    const linkedChildren =
                      selectedRun.linked_child_runs_by_step?.[step.id] ?? [];
                    const inputSummary = getStepInputSummary(
                      step.parsedInput,
                      index === 0,
                    );
                    const summary = getStepSummary(step.parsedOutput, step.error);
                    const toolResults = getToolResultsFromNextStep(
                      selectedRun.steps,
                      index,
                    );

                    return (
                      <Collapsible
                        key={step.id}
                        open={isExpanded}
                        onOpenChange={() => toggleStep(step.id)}
                      >
                        <Card
                          className={`overflow-hidden py-0 ${isActiveStep ? 'ring-1 ring-blue-500/50' : ''
                            }`}
                        >
                          <CollapsibleTrigger asChild>
                            <button className="flex w-full items-center justify-between px-4 py-3 text-left transition-colors hover:bg-accent/50">
                              <div className="min-w-0 flex items-start gap-3">
                                <span className="mt-0.5 w-5 text-[11px] font-mono text-muted-foreground/70">
                                  {step.step_number}
                                </span>
                                <div className="min-w-0">
                                  <StepSummary
                                    inputSummary={inputSummary}
                                    summary={summary}
                                    step={step}
                                  />
                                </div>
                              </div>
                              <div className="ml-4 flex items-center gap-4">
                                {step.parsedUsage && (
                                  <Tooltip>
                                    <TooltipTrigger asChild>
                                      <span className="text-[11px] text-muted-foreground font-mono">
                                        {formatInputTokenSummary(step.parsedUsage)}
                                      </span>
                                    </TooltipTrigger>
                                    <TooltipContent>
                                      <TokenBreakdownTooltip
                                        input={getInputTokenBreakdown(
                                          step.parsedUsage.inputTokens as
                                          | number
                                          | InputTokenBreakdown
                                          | null
                                          | undefined,
                                        )}
                                        output={getOutputTokenBreakdown(
                                          step.parsedUsage.outputTokens as
                                          | number
                                          | OutputTokenBreakdown
                                          | null
                                          | undefined,
                                        )}
                                        raw={step.parsedUsage.raw}
                                      />
                                    </TooltipContent>
                                  </Tooltip>
                                )}
                                <span className="text-[11px] text-muted-foreground font-mono">
                                  {formatDuration(step.duration_ms)}
                                </span>
                                <ChevronDown
                                  className={`size-4 text-muted-foreground transition-transform ${isExpanded ? 'rotate-180' : ''
                                    }`}
                                />
                              </div>
                            </button>
                          </CollapsibleTrigger>

                          <CollapsibleContent>
                            <StepConfigBar
                              modelId={step.model_id}
                              provider={step.provider}
                              input={step.parsedInput}
                              providerOptions={step.parsedProviderOptions}
                              usage={step.parsedUsage}
                            />

                            {linkedChildren.length > 0 && (
                              <div className="border-t border-border px-4 py-3">
                                <div className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                                  Linked Child Runs
                                </div>
                                <div className="space-y-1.5 rounded-md border border-border/50 bg-background/50 p-2">
                                  {linkedChildren.map(child => (
                                    <button
                                      key={child.id}
                                      className="flex w-full items-start justify-between rounded-md border border-border/50 bg-background/50 px-3 py-2 text-left transition-colors hover:bg-accent/50"
                                      onClick={() => {
                                        selectRun(child.id).catch(setViewerError);
                                      }}
                                    >
                                      <div className="min-w-0">
                                        <div className="mb-1 flex items-center gap-2">
                                          <span className="text-[13px] font-medium text-foreground">
                                            {child.title}
                                          </span>
                                          <RunStatusBadge run={child} compact />
                                        </div>
                                        <div className="line-clamp-1 text-[11px] text-muted-foreground">
                                          {stripMarkdownForPreview(
                                            child.summary ||
                                            child.input_preview ||
                                            'No summary yet.',
                                          )}
                                        </div>
                                      </div>
                                      <ExternalLink className="ml-3 mt-0.5 size-4 shrink-0 text-muted-foreground" />
                                    </button>
                                  ))}
                                </div>
                              </div>
                            )}

                            <div className="grid min-w-0 grid-cols-2 divide-x divide-border border-t border-border">
                              <div className="min-w-0 bg-card/50">
                                <InputPanel input={step.parsedInput} />
                              </div>
                              <div className="min-w-0 p-4">
                                <div className="mb-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                                  Output
                                </div>
                                {step.error ? (
                                  <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 font-mono text-sm text-destructive-foreground">
                                    {step.error}
                                  </div>
                                ) : step.parsedOutput ? (
                                  <OutputDisplay
                                    output={step.parsedOutput}
                                    toolResults={toolResults}
                                  />
                                ) : (
                                  <div className="text-sm text-muted-foreground">
                                    No output
                                  </div>
                                )}
                              </div>
                            </div>

                            {step.parsedUsage && (
                              <div className="border-t border-border px-4 py-3">
                                <UsageDetails usage={step.parsedUsage} />
                              </div>
                            )}

                            <RawDataSection step={step} />
                          </CollapsibleContent>
                        </Card>
                      </Collapsible>
                    );
                  })}
                </div>
              </div>
            )}
          </ScrollArea>
        </main>
      </div>
    </div>
  );
}

function formatInputTokenSummary(usage: Record<string, unknown>) {
  const input = getInputTokenBreakdown(
    usage.inputTokens as number | InputTokenBreakdown | null | undefined,
  );
  const output = getOutputTokenBreakdown(
    usage.outputTokens as number | OutputTokenBreakdown | null | undefined,
  );
  const inputLabel =
    input.cacheRead && input.cacheRead > 0
      ? `${input.total} (${input.cacheRead} cached)`
      : String(input.total);
  const outputLabel =
    output.reasoning && output.reasoning > 0
      ? `${output.total} (${output.reasoning} reasoning)`
      : String(output.total);
  return `${inputLabel} → ${outputLabel}`;
}

function clampSidebarWidth(width: number) {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, width));
}

function findAncestorRunIDs(
  nodes: RunNode[],
  targetRunID: string,
  ancestors: string[] = [],
): string[] | null {
  for (const node of nodes) {
    if (node.id === targetRunID) {
      return ancestors;
    }

    const nextAncestors = findAncestorRunIDs(node.children, targetRunID, [
      ...ancestors,
      node.id,
    ]);
    if (nextAncestors) {
      return nextAncestors;
    }
  }

  return null;
}

export default App;
