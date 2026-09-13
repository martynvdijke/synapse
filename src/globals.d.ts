// Only Bootstrap (loaded from CDN) remains as a real global.
// All former window.X function bridges have been replaced with explicit imports.

// Bootstrap (loaded from CDN via HTML)
declare namespace bootstrap {
  class Modal {
    constructor(element: Element, options?: Record<string, unknown>);
    show(): void;
    hide(): void;
    static getInstance(element: Element): Modal | null;
  }
  class Tab {
    constructor(element: Element);
    show(): void;
    static getInstance(element: Element): Tab | null;
  }
  class Collapse {
    constructor(element: Element, options?: Record<string, unknown>);
    show(): void;
    hide(): void;
    static getInstance(element: Element): Collapse | null;
    static getOrCreateInstance(element: Element): Collapse;
  }
}
