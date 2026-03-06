import '@testing-library/jest-dom/vitest';

// Radix UI (ScrollArea) uses ResizeObserver; jsdom doesn't provide it.
class MockResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

// @ts-expect-error assign to global
globalThis.ResizeObserver = MockResizeObserver;

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() {
      return false;
    },
  }),
});

// EventSource mock (App subscribes to /api/events).
class MockEventSource {
  static instances: MockEventSource[] = [];

  url: string;
  readyState: number = 1;
  onerror: ((this: EventSource, ev: Event) => any) | null = null;
  private listeners: Map<string, Array<(event: any) => void>> = new Map();

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (event: any) => void) {
    const next = this.listeners.get(type) ?? [];
    next.push(listener);
    this.listeners.set(type, next);
  }

  removeEventListener(type: string, listener: (event: any) => void) {
    const next = this.listeners.get(type) ?? [];
    this.listeners.set(
      type,
      next.filter(item => item !== listener),
    );
  }

  close() {
    this.readyState = 2;
  }

  // Helper for tests.
  emit(type: string, event: any = {}) {
    const next = this.listeners.get(type) ?? [];
    next.forEach(listener => listener(event));
  }
}

// @ts-expect-error assign to global
globalThis.EventSource = MockEventSource;

