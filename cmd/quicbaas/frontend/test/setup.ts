import '@testing-library/jest-dom';

// Polyfill IntersectionObserver for jsdom.
class MockIntersectionObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Object.defineProperty(window, 'IntersectionObserver', {
  writable: true,
  configurable: true,
  value: MockIntersectionObserver,
});

// Polyfill scrollIntoView.
Element.prototype.scrollIntoView = () => {};

// Polyfill Element.prototype.animate for Svelte fly/fade transitions in jsdom.
// jsdom does not implement the Web Animations API; return a minimal stub so
// transition directives don't throw "element.animate is not a function".
if (typeof Element.prototype.animate !== 'function') {
  Element.prototype.animate = function () {
    return {
      finished: Promise.resolve(),
      cancel: () => {},
      pause: () => {},
      play: () => {},
      finish: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      onfinish: null,
      oncancel: null,
    } as unknown as Animation;
  };
}

// Polyfill localStorage.
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, val: string) => { store[key] = val; },
    removeItem: (key: string) => { delete store[key]; },
    clear: () => { store = {}; },
  };
})();
Object.defineProperty(window, 'localStorage', { value: localStorageMock });
