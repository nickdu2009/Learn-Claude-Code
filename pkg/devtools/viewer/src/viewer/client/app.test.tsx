import React from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import App from './app';
import { RunHeader, RunTreeItem } from './components/viewer/run-tree';
import { InputMessagePreview, MessageBubble, ToolDefinitionCard } from './components/viewer/step-inspector';

type FetchResponse = {
  ok: boolean;
  status: number;
  json: () => Promise<any>;
};

function jsonResponse(payload: any, status = 200): FetchResponse {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => payload,
  };
}

function createFetchMock(handlers: Record<string, () => FetchResponse>) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString();
    const handler = handlers[url];
    if (!handler) {
      return jsonResponse({ error: 'unhandled', url }, 500);
    }
    return handler();
  });
  // @ts-expect-error assign to global
  globalThis.fetch = fetchMock;
  return fetchMock;
}

function buildInput(prompt: any[], extra: Record<string, unknown> = {}) {
  return JSON.stringify({
    prompt,
    ...extra,
  });
}

function buildOutput(content: any[], extra: Record<string, unknown> = {}) {
  return JSON.stringify({
    content,
    ...extra,
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe('viewer App interactions', () => {
  it('renders unsupported-version page when /api/trace/meta reports unsupported', async () => {
    createFetchMock({
      '/api/trace/meta': () =>
        jsonResponse({
          supported: false,
          version: 1,
          message: 'Unsupported trace version 1',
        }),
    });

    const user = userEvent.setup();
    render(<App />);

    expect(await screen.findByText('Unsupported Trace')).toBeInTheDocument();
    expect(screen.getByText(/Unsupported trace version 1/i)).toBeInTheDocument();
  });

  it('supports run tree navigation and parent link jump', async () => {
    const runTree = [
      {
        id: 'root-run',
        kind: 'main',
        title: 'Root',
        status: 'completed',
        completion_reason: 'normal',
        started_at: '2026-03-06T12:00:00Z',
        finished_at: '2026-03-06T12:00:05Z',
        step_count: 1,
        child_count: 1,
        children: [
          {
            id: 'child-run',
            kind: 'subagent',
            title: 'Child',
            status: 'completed',
            completion_reason: 'normal',
            started_at: '2026-03-06T12:00:01Z',
            finished_at: '2026-03-06T12:00:04Z',
            step_count: 1,
            child_count: 0,
            children: [],
          },
        ],
      },
    ];

    createFetchMock({
      '/api/trace/meta': () =>
        jsonResponse({
          supported: true,
          version: 2,
          generated_at: '2026-03-06T12:00:06Z',
        }),
      '/api/runs': () => jsonResponse(runTree),
      '/api/runs/root-run': () =>
        jsonResponse({
          run: runTree[0],
          steps: [
            {
              id: 'root-step-1',
              run_id: 'root-run',
              step_number: 1,
              type: 'generate',
              model_id: 'mock',
              provider: 'mock-provider',
              started_at: '2026-03-06T12:00:00Z',
              duration_ms: 10,
              input: buildInput([
                { role: 'system', content: 'System prompt' },
                { role: 'user', content: 'Root question' },
              ]),
              output: buildOutput([{ type: 'text', text: 'Root answer' }]),
              usage: JSON.stringify({ inputTokens: 12, outputTokens: 4 }),
              error: null,
              raw_request: JSON.stringify({ request: true }),
              raw_response: JSON.stringify({ response: true }),
              raw_chunks: null,
              provider_options: JSON.stringify({ mode: 'test' }),
              linked_child_run_ids: [],
            },
          ],
          linked_child_runs_by_step: {},
          parent: null,
        }),
      '/api/runs/child-run': () =>
        jsonResponse({
          run: runTree[0].children[0],
          steps: [
            {
              id: 'child-step-1',
              run_id: 'child-run',
              step_number: 1,
              type: 'generate',
              model_id: 'mock',
              provider: 'mock-provider',
              started_at: '2026-03-06T12:00:02Z',
              duration_ms: 10,
              input: buildInput([
                { role: 'system', content: 'Child system prompt' },
                { role: 'user', content: 'Child question' },
              ]),
              output: buildOutput([{ type: 'text', text: 'Child answer' }]),
              usage: JSON.stringify({ inputTokens: 10, outputTokens: 2 }),
              error: null,
              raw_request: JSON.stringify({ request: true }),
              raw_response: JSON.stringify({ response: true }),
              raw_chunks: null,
              provider_options: JSON.stringify({ mode: 'test' }),
              linked_child_run_ids: [],
            },
          ],
          linked_child_runs_by_step: {},
          parent: runTree[0],
        }),
    });

    const user = userEvent.setup();
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Root' })).toBeInTheDocument();

    const childRunButton = (await screen.findAllByRole('button')).find(
      button =>
        button.textContent?.includes('Child') && button.textContent?.includes('subagent'),
    );
    expect(childRunButton).toBeDefined();
    await user.click(childRunButton!);
    expect(await screen.findByRole('heading', { name: 'Child' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /parent:\s*Root/i })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /parent:\s*Root/i }));
    expect(await screen.findByRole('heading', { name: 'Root' })).toBeInTheDocument();
  });

  it('keeps the run tree pane fixed while the detail pane can shrink', async () => {
    const user = userEvent.setup();

    const runTree = [
      {
        id: 'root-run',
        kind: 'main',
        title: 'Root',
        status: 'completed',
        completion_reason: 'normal',
        started_at: '2026-03-06T12:00:00Z',
        finished_at: '2026-03-06T12:00:05Z',
        step_count: 1,
        child_count: 0,
        children: [],
      },
    ];

    createFetchMock({
      '/api/trace/meta': () =>
        jsonResponse({
          supported: true,
          version: 2,
          generated_at: '2026-03-06T12:00:06Z',
        }),
      '/api/runs': () => jsonResponse(runTree),
      '/api/runs/root-run': () =>
        jsonResponse({
          run: runTree[0],
          steps: [
            {
              id: 'root-step-1',
              run_id: 'root-run',
              step_number: 1,
              type: 'generate',
              model_id: 'mock',
              provider: 'mock-provider',
              started_at: '2026-03-06T12:00:00Z',
              duration_ms: 10,
              input: buildInput([
                { role: 'system', content: 'System prompt' },
                {
                  role: 'user',
                  content:
                    'A deliberately long message that should stay inside the detail pane without pushing the run tree out of the layout.',
                },
              ]),
              output: buildOutput([{ type: 'text', text: 'Root answer' }]),
              usage: JSON.stringify({ inputTokens: 12, outputTokens: 4 }),
              error: null,
              raw_request: JSON.stringify({ request: true }),
              raw_response: JSON.stringify({ response: true }),
              raw_chunks: null,
              provider_options: JSON.stringify({ mode: 'test' }),
              linked_child_run_ids: [],
            },
          ],
          linked_child_runs_by_step: {},
          parent: null,
        }),
    });

    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Root' })).toBeInTheDocument();

    const runTreePane = screen.getByText('Runs').closest('aside');
    expect(runTreePane).toHaveClass('min-h-0', 'shrink-0');
    expect(runTreePane).toHaveStyle({ width: '340px' });
    expect(screen.getByText('Execution tree')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /collapse runs sidebar/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /resize runs sidebar/i })).toBeInTheDocument();

    const detailPane = document.querySelector('main');
    expect(detailPane).toHaveClass('min-h-0', 'min-w-0', 'flex-1');

    const runTreeScrollArea = runTreePane?.querySelector('[data-slot="scroll-area"]');
    expect(runTreeScrollArea).toHaveClass('min-h-0', 'flex-1', 'overflow-hidden');

    const splitPane = runTreePane?.parentElement;
    expect(splitPane).toHaveClass('min-w-0', 'flex', 'overflow-hidden');

    const resizeHandle = screen.getByRole('button', { name: /resize runs sidebar/i });
    fireEvent.mouseDown(resizeHandle, { clientX: 340 });
    fireEvent.mouseMove(window, { clientX: 400 });
    fireEvent.mouseUp(window);
    expect(runTreePane).toHaveStyle({ width: '400px' });

    await user.click(screen.getByRole('button', { name: /collapse runs sidebar/i }));
    expect(runTreePane).toHaveStyle({ width: '56px' });
    expect(screen.queryByText('Execution tree')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /expand runs sidebar/i })).toBeInTheDocument();
  });

  it('renders hybrid step inspector, linked child runs, and clear flow', async () => {
    let cleared = false;
    const runTree = [
      {
        id: 'root-run',
        kind: 'main',
        title: 'Root',
        status: 'completed',
        completion_reason: 'normal',
        started_at: '2026-03-06T12:00:00Z',
        finished_at: '2026-03-06T12:00:05Z',
        step_count: 2,
        child_count: 1,
        children: [
          {
            id: 'child-a',
            kind: 'subagent',
            title: 'Child A',
            status: 'completed',
            completion_reason: 'normal',
            started_at: '2026-03-06T12:00:01Z',
            finished_at: '2026-03-06T12:00:02Z',
            step_count: 1,
            child_count: 0,
            children: [],
            summary: '## Child summary\n- delegated review',
          },
        ],
      },
    ];

    createFetchMock({
      '/api/trace/meta': () =>
        jsonResponse({
          supported: true,
          version: 2,
          generated_at: '2026-03-06T12:00:06Z',
        }),
      '/api/runs': () => jsonResponse(cleared ? [] : runTree),
      '/api/runs/root-run': () =>
        jsonResponse({
          run: runTree[0],
          steps: [
            {
              id: 'step-1',
              run_id: 'root-run',
              step_number: 1,
              type: 'generate',
              model_id: 'mock',
              provider: 'dashscope',
              started_at: '2026-03-06T12:00:00Z',
              duration_ms: 25,
              input: buildInput(
                [
                  { role: 'system', content: 'You are helpful.' },
                  { role: 'user', content: 'Create delegated.txt with subagent-success' },
                ],
                {
                  tools: [
                    {
                      name: 'write_file',
                      description: '## Write file\n- create files\nUse `utf-8`.',
                      parameters: { type: 'object' },
                    },
                  ],
                  temperature: 0.2,
                  maxOutputTokens: 200,
                },
              ),
              output: buildOutput(
                [
                  {
                    type: 'tool-call',
                    toolName: 'write_file',
                    toolCallId: 'call-write',
                    args: {
                      path: '/tmp/delegated.txt',
                      content: 'subagent-success',
                    },
                  },
                ],
                { finishReason: 'tool-calls' },
              ),
              usage: JSON.stringify({
                inputTokens: { total: 100, cacheRead: 40, noCache: 60 },
                outputTokens: { total: 12, text: 8, reasoning: 4 },
                raw: { completion_tokens: 12 },
              }),
              error: null,
              raw_request: JSON.stringify({ messages: [] }),
              raw_response: JSON.stringify({ choices: [] }),
              raw_chunks: null,
              provider_options: JSON.stringify({ baseURL: 'https://example.test' }),
              linked_child_run_ids: ['child-a'],
            },
            {
              id: 'step-2',
              run_id: 'root-run',
              step_number: 2,
              type: 'generate',
              model_id: 'mock',
              provider: 'dashscope',
              started_at: '2026-03-06T12:00:01Z',
              duration_ms: 20,
              input: buildInput([
                { role: 'system', content: 'You are helpful.' },
                { role: 'user', content: 'Create delegated.txt with subagent-success' },
                {
                  role: 'assistant',
                  content: [
                    {
                      type: 'tool-call',
                      toolName: 'write_file',
                      toolCallId: 'call-write',
                      args: {
                        path: '/tmp/delegated.txt',
                        content: 'subagent-success',
                      },
                    },
                  ],
                },
                {
                  role: 'tool',
                  content: [
                    {
                      type: 'tool-result',
                      toolName: 'write_file',
                      toolCallId: 'call-write',
                      result: 'Successfully wrote to /tmp/delegated.txt',
                    },
                  ],
                },
              ]),
              output: buildOutput([{ type: 'text', text: 'Verification succeeded.' }]),
              usage: JSON.stringify({
                inputTokens: 50,
                outputTokens: 7,
              }),
              error: null,
              raw_request: JSON.stringify({ messages: [] }),
              raw_response: JSON.stringify({ choices: [] }),
              raw_chunks: null,
              provider_options: JSON.stringify({ baseURL: 'https://example.test' }),
              linked_child_run_ids: [],
            },
          ],
          linked_child_runs_by_step: {
            'step-1': [runTree[0].children[0]],
          },
          parent: null,
        }),
      '/api/runs/child-a': () =>
        jsonResponse({
          run: runTree[0].children[0],
          steps: [
            {
              id: 'child-step-1',
              run_id: 'child-a',
              step_number: 1,
              type: 'generate',
              model_id: 'mock',
              provider: 'dashscope',
              started_at: '2026-03-06T12:00:02Z',
              duration_ms: 15,
              input: buildInput([
                { role: 'system', content: 'Child system' },
                { role: 'user', content: 'Do the work' },
              ]),
              output: buildOutput([{ type: 'text', text: 'Child done.' }]),
              usage: JSON.stringify({ inputTokens: 10, outputTokens: 2 }),
              error: null,
              raw_request: JSON.stringify({ messages: [] }),
              raw_response: JSON.stringify({ choices: [] }),
              raw_chunks: null,
              provider_options: JSON.stringify({ baseURL: 'https://example.test' }),
              linked_child_run_ids: [],
            },
          ],
          linked_child_runs_by_step: {},
          parent: runTree[0],
        }),
      '/api/clear': () => {
        cleared = true;
        return jsonResponse({ ok: true });
      },
    });

    const user = userEvent.setup();
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Root' })).toBeInTheDocument();
    expect(screen.getByText('Child A')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /collapse child runs for root/i }));
    expect(screen.queryByText('Child A')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /expand child runs for root/i }));
    expect(screen.getByText('Child A')).toBeInTheDocument();

    await user.click(screen.getAllByRole('button', { name: /write_file/i })[0]!);

    expect(await screen.findByText('Linked Child Runs')).toBeInTheDocument();
    expect(screen.getByText(/Child summary delegated review/i)).toBeInTheDocument();
    expect(screen.queryByText(/## Child summary/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /1 available tool/i })).toBeInTheDocument();
    expect(screen.getByText('Input Tokens')).toBeInTheDocument();
    expect(screen.getByText('Output Tokens')).toBeInTheDocument();
    expect(screen.getByText('Raw Provider Usage')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Request \/ Response/i })).toBeInTheDocument();
    expect(screen.getAllByText(/write_file/i).length).toBeGreaterThan(0);

    await user.click(
      screen.getByRole('button', { name: /Open all input messages \(2\)/i }),
    );
    expect(await screen.findByText('All Messages (2)')).toBeInTheDocument();
    await user.keyboard('{Escape}');

    await user.click(screen.getByRole('button', { name: /Open child runs/i, hidden: true }));
    expect(await screen.findByText('Child Runs (1)')).toBeInTheDocument();
    expect(screen.getAllByText(/Child summary delegated review/i)).toHaveLength(2);
    await user.keyboard('{Escape}');

    await user.click(screen.getByRole('button', { name: /Clear/i, hidden: true }));
    await waitFor(() => {
      expect(screen.getByText('No runs yet')).toBeInTheDocument();
    });
  });

  it('fails fast when step payloads violate the strict trace contract', async () => {
    const runTree = [
      {
        id: 'root-run',
        kind: 'main',
        title: 'Root',
        status: 'completed',
        completion_reason: 'normal',
        started_at: '2026-03-06T12:00:00Z',
        finished_at: '2026-03-06T12:00:05Z',
        step_count: 1,
        child_count: 0,
        children: [],
      },
    ];

    createFetchMock({
      '/api/trace/meta': () => jsonResponse({ supported: true, version: 2 }),
      '/api/runs': () => jsonResponse(runTree),
      '/api/runs/root-run': () =>
        jsonResponse({
          run: runTree[0],
          steps: [
            {
              id: 'step-1',
              run_id: 'root-run',
              step_number: 1,
              type: 'generate',
              model_id: 'mock',
              provider: 'dashscope',
              started_at: '2026-03-06T12:00:00Z',
              duration_ms: 10,
              input: '{}',
              output: null,
              usage: null,
              error: null,
              raw_request: null,
              raw_response: null,
              raw_chunks: null,
              provider_options: null,
              linked_child_run_ids: [],
            },
          ],
          linked_child_runs_by_step: {},
          parent: null,
        }),
    });

    render(<App />);

    expect(await screen.findByText('Viewer Error')).toBeInTheDocument();
    expect(screen.getByText(/step 1\.input\.prompt must be an array/i)).toBeInTheDocument();
  });

  it('expands tool-only messages by default in the message drawer', () => {
    render(
      <MessageBubble
        index={22}
        message={{
          role: 'tool',
          content: [
            {
              type: 'tool-result',
              toolName: 'todo',
              toolCallId: 'call-b931557fc70d406e8a7c69',
              result:
                "[x] #create_main_py: Create main.py with 'Hello, World!' print statement\n\n(4/4 completed)",
            },
          ],
        }}
      />,
    );

    expect(screen.getByText('Output')).toBeInTheDocument();
    expect(screen.getByText(/\(4\/4 completed\)/)).toBeInTheDocument();
  });

  it('renders markdown for text and reasoning content', async () => {
    const user = userEvent.setup();

    render(
      <MessageBubble
        message={{
          role: 'system',
          content: [
            {
              type: 'reasoning',
              text: '## Plan\n- inspect state\n- render output',
            },
            {
              type: 'text',
              text: '# Summary\nUse `react-markdown`.\n\n[Docs](https://example.com/docs)',
            },
          ],
        }}
      />,
    );

    expect(
      screen.getByRole('heading', {
        name: 'Summary',
        level: 1,
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Docs' })).toHaveAttribute(
      'href',
      'https://example.com/docs',
    );
    expect(screen.getByText('react-markdown', { selector: 'code' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Thinking/i }));

    expect(
      screen.getByRole('heading', {
        name: 'Plan',
        level: 2,
      }),
    ).toBeInTheDocument();
    expect(screen.getByText('inspect state')).toBeInTheDocument();
    expect(screen.getByText('render output')).toBeInTheDocument();
  });

  it('allows re-expanding tool-only messages after collapsing them', async () => {
    const user = userEvent.setup();

    render(
      <MessageBubble
        index={22}
        message={{
          role: 'tool',
          content: [
            {
              type: 'tool-result',
              toolName: 'todo',
              toolCallId: 'call-b931557fc70d406e8a7c69',
              result:
                "[x] #create_main_py: Create main.py with 'Hello, World!' print statement\n\n(4/4 completed)",
            },
          ],
        }}
      />,
    );

    const toggleButton = screen.getByRole('button', { name: /Result · todo/i });

    expect(screen.getByText('Output')).toBeInTheDocument();
    await user.click(toggleButton);
    expect(screen.queryByText('Output')).not.toBeInTheDocument();

    await user.click(toggleButton);
    expect(screen.getByText('Output')).toBeInTheDocument();
    expect(screen.getByText(/\(4\/4 completed\)/)).toBeInTheDocument();
  });

  it('renders markdown for string tool results and keeps object results as JSON', () => {
    render(
      <>
        <MessageBubble
          message={{
            role: 'tool',
            content: [
              {
                type: 'tool-result',
                toolName: 'todo',
                toolCallId: 'call-markdown',
                result: '- [x] shipped markdown viewer',
              },
            ],
          }}
        />
        <MessageBubble
          message={{
            role: 'tool',
            content: [
              {
                type: 'tool-result',
                toolName: 'json-tool',
                toolCallId: 'call-json',
                result: {
                  status: 'ok',
                  items: ['a', 'b'],
                },
              },
            ],
          }}
        />
      </>,
    );

    const checkedItems = screen.getAllByRole('checkbox');
    expect(checkedItems).toHaveLength(1);
    expect(checkedItems[0]).toBeChecked();
    expect(screen.getByText('shipped markdown viewer')).toBeInTheDocument();
    expect(screen.getByText(/"status": "ok"/)).toBeInTheDocument();
    expect(screen.getByText(/"items": \[/)).toBeInTheDocument();
  });

  it('wraps long run titles and expands summary on click', () => {
    render(
      <RunTreeItem
        node={{
          id: 'long-run',
          kind: 'subagent',
          title:
            'This is an extremely long run title intended to verify that the tree item truncates instead of expanding the sidebar width',
          status: 'completed',
          completion_reason: 'normal',
          started_at: '2026-03-06T12:00:00Z',
          finished_at: '2026-03-06T12:00:05Z',
          step_count: 12,
          child_count: 0,
          summary:
            'This is an equally long summary preview that should remain clipped within the run tree item instead of stretching the layout horizontally.',
          children: [],
        }}
        depth={0}
        selectedRunID={null}
        collapsedRunIDs={new Set()}
        onSelect={() => { }}
        onToggleCollapse={() => { }}
      />,
    );

    const treeButton = screen.getByRole('button', { name: /This is an extremely long run title/i });
    expect(treeButton).not.toHaveClass('overflow-hidden');

    const title = screen.getByText(
      /This is an extremely long run title intended to verify that the tree item truncates/i,
    );
    expect(title).toHaveClass('whitespace-normal', 'break-all');

    const preview = screen.getByText(
      /This is an equally long summary preview that should remain clipped/i,
    );
    expect(preview).toHaveClass('whitespace-normal', 'break-all');
    expect(preview.getAttribute('style')).toContain('display: -webkit-box;');
    expect(preview.getAttribute('style')).toContain('-webkit-line-clamp: 2;');
    expect(preview.getAttribute('style')).toContain('overflow: hidden;');

    const showMoreButton = within(treeButton).getByRole('button', { name: /Show more/i });
    fireEvent.click(showMoreButton);
    expect(preview.getAttribute('style') ?? '').toBe('');

    const showLessButton = within(treeButton).getByRole('button', { name: /Show less/i });
    fireEvent.click(showLessButton);
    expect(preview.getAttribute('style')).toContain('-webkit-line-clamp: 2;');
  });

  it('renders markdown in the run header summary', () => {
    render(
      <RunHeader
        run={{
          id: 'run-1',
          kind: 'main',
          title: 'Markdown Run',
          status: 'completed',
          completion_reason: 'normal',
          started_at: '2026-03-06T12:00:00Z',
          finished_at: '2026-03-06T12:00:05Z',
          step_count: 1,
          summary: '## Goal\nShip `markdown` support.\n\n- text\n- reasoning',
        }}
        parent={null}
        steps={[]}
        onSelectParent={() => { }}
      />,
    );

    expect(
      screen.getByRole('heading', {
        name: 'Goal',
        level: 2,
      }),
    ).toBeInTheDocument();
    expect(screen.getByText('markdown', { selector: 'code' })).toBeInTheDocument();
    expect(screen.getByText('text')).toBeInTheDocument();
    expect(screen.getByText('reasoning')).toBeInTheDocument();
  });

  it('collapses long run header markdown by default and can expand it', async () => {
    const user = userEvent.setup();

    render(
      <RunHeader
        run={{
          id: 'run-2',
          kind: 'main',
          title: 'Long Markdown Run',
          status: 'completed',
          completion_reason: 'normal',
          started_at: '2026-03-06T12:00:00Z',
          finished_at: '2026-03-06T12:00:05Z',
          step_count: 1,
          summary: Array.from({ length: 14 }, (_, index) => `${index + 1}. item ${index + 1}`).join('\n'),
        }}
        parent={null}
        steps={[]}
        onSelectParent={() => { }}
      />,
    );

    expect(screen.getByRole('button', { name: /show more/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /show less/i })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /show more/i }));

    expect(screen.getByRole('button', { name: /show less/i })).toBeInTheDocument();
    expect(screen.getByText('item 14')).toBeInTheDocument();
  });

  it('renders markdown for tool descriptions and keeps collapsed previews plain', async () => {
    const user = userEvent.setup();

    render(
      <ToolDefinitionCard
        tool={{
          name: 'write_file',
          description: '## Write file\n- create files\nUse `utf-8`.',
          parameters: { type: 'object' },
        }}
      />,
    );

    expect(screen.getByText(/Write file create files Use utf-8\./i)).toBeInTheDocument();
    expect(screen.queryByText(/## Write file/i)).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /write_file/i }));

    expect(screen.getByRole('heading', { name: 'Write file', level: 2 })).toBeInTheDocument();
    expect(screen.getByRole('listitem')).toHaveTextContent(/create files/i);
    expect(screen.getByText('utf-8', { selector: 'code' })).toBeInTheDocument();
  });

  it('strips markdown syntax from message previews', () => {
    render(
      <InputMessagePreview
        message={{
          role: 'user',
          content: [
            {
              type: 'text',
              text: '# Title\n- item one\n[Docs](https://example.com)',
            },
          ],
        }}
      />,
    );

    const preview = screen.getByText(/Title item one Docs/i);
    expect(preview).toBeInTheDocument();
    expect(preview.textContent).toBe('Title item one Docs');
  });

  it('strips markdown syntax from run tree previews', () => {
    render(
      <RunTreeItem
        node={{
          id: 'markdown-run',
          kind: 'main',
          title: 'Markdown Preview',
          status: 'completed',
          completion_reason: 'normal',
          started_at: '2026-03-06T12:00:00Z',
          finished_at: '2026-03-06T12:00:05Z',
          step_count: 1,
          child_count: 0,
          summary: '## Goal\n- first item\n- second item',
          children: [],
        }}
        depth={0}
        selectedRunID={null}
        collapsedRunIDs={new Set()}
        onSelect={() => { }}
        onToggleCollapse={() => { }}
      />,
    );

    const card = screen.getByRole('button');
    const preview = within(card).getByText(/Goal first item second item/i);
    expect(preview).toBeInTheDocument();
    expect(preview.textContent).toBe('Goal first item second item');
  });
});

