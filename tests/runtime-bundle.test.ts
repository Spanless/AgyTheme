import { describe, expect, test } from 'bun:test';
import vm from 'node:vm';
import { createSandbox, goBundle, readRuntimePart, sortedFiles, RUNTIME_DIR, type Modules } from './helpers.ts';

const PLACEHOLDER = '})(__AG_BOOT_PAYLOAD__);';

describe('hand-assembled partial runtime bundles', () => {
  const scenarios: { name: string; parts: string[]; modules: Modules }[] = [
    {
      name: 'i18n-only',
      parts: ['00-head.js', '60-i18n-engine.js', '90-tail.js'],
      modules: { theme: false, i18n: true, metrics: false, deadlockHealer: false, zoomShortcuts: false }
    },
    {
      name: 'theme-only',
      parts: ['00-head.js', '20-theme-sync.js', '90-tail.js'],
      modules: { theme: true, i18n: false, metrics: false, deadlockHealer: false, zoomShortcuts: false }
    },
    {
      name: 'metrics-only',
      parts: ['00-head.js', '30-antigravity-probe.js', '40-status-machine.js', '45-telemetry-render.js', '50-telemetry-ui.js', '90-tail.js'],
      modules: { theme: false, i18n: false, metrics: true, deadlockHealer: false, zoomShortcuts: false }
    }
  ];

  for (const sc of scenarios) {
    test(`${sc.name} boots and mounts #agytheme-style`, () => {
      const boot = {
        version: '1.0.0-test',
        platform: 'darwin',
        enabled: true,
        preset: 'light',
        cssText: sc.modules.theme ? 'body { color: #000; }' : '',
        customCss: '',
        themeVars: {},
        i18nDict: sc.modules.i18n ? { Settings: '设置' } : {},
        modules: sc.modules,
        selectors: {},
        userConfig: { modules: sc.modules },
        customJs: ''
      };
      const sandbox = createSandbox({ __AG_BOOT_PAYLOAD__: boot });
      const code = sc.parts.map(readRuntimePart).join('\n');
      vm.runInContext(`const __AG_BOOT_PAYLOAD__ = globalThis.__AG_BOOT_PAYLOAD__;\n${code}`, sandbox);
      expect(sandbox.__ag_patch_version).toBe('1.0.0-test');
      const mounted = sandbox.headChildren.find((el) => el.id === 'agytheme-style');
      expect(mounted?.textContent.length ?? 0).toBeGreaterThan(0);
      expect(sandbox.docAttrs.get('data-ag-preset')).toBe(sc.modules.theme ? 'light' : undefined);
    });
  }
});

describe('Go agytheme bundle output', () => {
  const runScript = (script: string) => {
    const sandbox = createSandbox();
    vm.runInContext(script, sandbox);
    return sandbox;
  };

  test('full bundle replaces the placeholder, parses and boots', () => {
    const rep = goBundle({ config: { preset: 'dark' } });
    expect(rep.script).not.toContain(PLACEHOLDER);
    expect(rep.preset).toBe('dark');
    expect(rep.patchVersion).toMatch(/^\d+\.\d+\.\d+.*-stable-[0-9a-f]{8}$/);
    const sandbox = runScript(rep.script);
    expect(sandbox.__ag_patch_version).toBe(rep.patchVersion);
    expect(rep.script).toContain('"preset":"dark"');
    expect(sandbox.docAttrs.get('data-ag-preset')).toMatch(/^(light|dark)$/);
    const css = sandbox.headChildren.find((el) => el.id === 'agytheme-style')?.textContent ?? '';
    expect(css).toContain('.ag-lm-ring-arc');
  });

  const gated: { name: string; features: Record<string, boolean>; on: string[]; off: string[] }[] = [
    { name: 'i18n only', features: { theme: false, i18n: true, metrics: false, fixes: false }, on: ['i18n'], off: ['theme', 'metrics', 'deadlockHealer', 'zoomShortcuts'] },
    { name: 'theme only', features: { theme: true, i18n: false, metrics: false, fixes: false }, on: ['theme'], off: ['i18n', 'metrics', 'deadlockHealer'] },
    { name: 'metrics only', features: { theme: false, i18n: false, metrics: true, fixes: false }, on: ['metrics'], off: ['theme', 'i18n', 'zoomShortcuts'] },
    { name: 'without fixes', features: { theme: true, i18n: true, metrics: true, fixes: false }, on: ['theme', 'i18n', 'metrics'], off: ['deadlockHealer', 'zoomShortcuts'] }
  ];

  for (const g of gated) {
    test(`feature-gated bundle (${g.name}) drops uninstalled modules and still boots`, () => {
      const rep = goBundle({ state: { version: '0.1.0', features: g.features }, config: { modules: { theme: true, i18n: true, metrics: true } } });
      for (const m of g.on) expect(rep.modules[m]).toBe(true);
      for (const m of g.off) expect(rep.modules[m]).toBe(false);
      const sandbox = runScript(rep.script);
      expect(sandbox.__ag_patch_version).toBe(rep.patchVersion);
      expect(sandbox.docAttrs.get('data-ag-preset')).toBe(g.features.theme ? 'light' : undefined);
    });
  }

  test('safe mode ignores user config', () => {
    const rep = goBundle({ config: { enabled: false, preset: 'dark' }, safeMode: true });
    expect(rep.enabled).toBe(true);
    expect(rep.preset).toBe('light');
    expect(runScript(rep.script).__ag_patch_version).toBe(rep.patchVersion);
  });
});

test('all runtime parts concatenate into syntactically valid JS', () => {
  const js = sortedFiles(RUNTIME_DIR, '.js').map(readRuntimePart).join('\n');
  expect(() => new vm.Script('const __AG_BOOT_PAYLOAD__ = {};\n' + js, { filename: 'runtime-bundle.js' })).not.toThrow();
  expect(js.split(PLACEHOLDER).length - 1).toBe(1);
});

test('idle unstarted conversation hides the pill instead of showing 就绪', () => {
  const parts = ['00-head.js', '30-antigravity-probe.js', '40-status-machine.js', '45-telemetry-render.js', '50-telemetry-ui.js', '90-tail.js'];
  const boot = {
    version: '1.0.0-test',
    platform: 'darwin',
    enabled: true,
    preset: 'light',
    cssText: '',
    customCss: '',
    themeVars: {},
    i18nDict: {},
    modules: { theme: false, i18n: false, metrics: true, deadlockHealer: false, zoomShortcuts: false },
    selectors: {},
    userConfig: {},
    customJs: ''
  };
  const createdIds: string[] = [];
  const rowAttrs = new Map<string, string>();
  const leftGroup = {
    children: [] as unknown[],
    contains: () => true,
    appendChild(el: unknown) {
      this.children.push(el);
    },
    getBoundingClientRect: () => ({ width: 120, height: 24, top: 0, left: 0 })
  };
  const rightGroup = {
    getBoundingClientRect: () => ({ width: 80, height: 24, top: 0, left: 0 })
  };
  const row = {
    children: [leftGroup, rightGroup],
    firstElementChild: leftGroup,
    lastElementChild: rightGroup,
    parentElement: null,
    setAttribute: (k: string, v: string) => rowAttrs.set(k, v),
    hasAttribute: (k: string) => rowAttrs.has(k),
    removeAttribute: (k: string) => rowAttrs.delete(k)
  };
  const modelTrigger = { parentElement: row };
  const inputBox = {
    id: 'antigravity.agentSidePanelInputBox',
    closest: () => null,
    contains: () => false,
    querySelector: (sel: string) => (sel.includes('model-selector-trigger') ? modelTrigger : null),
    getBoundingClientRect: () => ({ width: 600, height: 80, top: 500, left: 0 })
  };
  const sandbox = createSandbox({ __AG_BOOT_PAYLOAD__: boot });
  const origCreate = sandbox.document.createElement.bind(sandbox.document);
  sandbox.document.createElement = (tag: string) => {
    const el = origCreate(tag);
    Object.defineProperty(el, 'id', {
      get: () => el._id || '',
      set: (v: string) => {
        el._id = v;
        if (v) createdIds.push(v);
      }
    });
    return el;
  };
  sandbox.document.querySelectorAll = (sel: string) =>
    sel.includes('antigravity.agentSidePanelInputBox') ? [inputBox] : [];
  sandbox.document.getElementById = (id: string) =>
    id === 'antigravity.agentSidePanelInputBox' ? inputBox : sandbox.headChildren.find((el: { id: string }) => el.id === id) || null;

  const code = parts.map(readRuntimePart).join('\n');
  vm.runInContext(`const __AG_BOOT_PAYLOAD__ = globalThis.__AG_BOOT_PAYLOAD__;\n${code}`, sandbox);
  const snap = sandbox.__ag_refresh_wait_ui();
  expect(snap.visualState).toBe('idle');
  expect(createdIds).not.toContain('ag-live-metrics-pill');
  expect(rowAttrs.has('data-ag-row-mode')).toBe(false);
  expect(readRuntimePart('50-telemetry-ui.js')).not.toContain("'就绪'");
});

test('telemetry pill recovers from bottom mode when composer widens after metrics update', () => {
  const parts = ['00-head.js', '30-antigravity-probe.js', '40-status-machine.js', '45-telemetry-render.js', '50-telemetry-ui.js', '90-tail.js'];
  const boot = {
    version: '1.0.0-test',
    platform: 'darwin',
    enabled: true,
    preset: 'dark',
    cssText: '',
    customCss: '',
    themeVars: {},
    i18nDict: {},
    modules: { theme: false, i18n: false, metrics: true, deadlockHealer: false, zoomShortcuts: false },
    selectors: {},
    userConfig: {},
    customJs: ''
  };

  type NodeLike = {
    tagName: string;
    id: string;
    className: string;
    textContent: string;
    innerHTML: string;
    parentElement: NodeLike | null;
    parentNode: NodeLike | null;
    children: NodeLike[];
    attrs: Map<string, string>;
    style: Record<string, string>;
    isConnected: boolean;
    classList: { contains: (cls: string) => boolean };
    setAttribute: (k: string, v: string) => void;
    getAttribute: (k: string) => string | null;
    removeAttribute: (k: string) => void;
    hasAttribute: (k: string) => boolean;
    addEventListener: () => void;
    closest: (sel: string) => NodeLike | null;
    contains: (el: NodeLike) => boolean;
    querySelector: (sel: string) => NodeLike | null;
    querySelectorAll: (sel: string) => NodeLike[];
    insertBefore: (child: NodeLike, ref: NodeLike | null) => NodeLike;
    appendChild: (child: NodeLike) => NodeLike;
    remove: () => void;
    cloneNode: () => NodeLike;
    getBoundingClientRect: () => { width: number; height: number; top: number; left: number };
    nextElementSibling: NodeLike | null;
    nextSibling: NodeLike | null;
    __agRaw?: string;
    _innerSpanBody?: NodeLike;
    _innerSpanState?: NodeLike;
    _innerSpanRing?: NodeLike;
    _innerSpanText?: NodeLike;
  };

  const elementsById = new Map<string, NodeLike>();
  const fitIntrinsicWidths: Record<string, number> = { full: 240, short: 190, units: 140, min: 84 };
  let rowWidth = 360;
  const roState: { cb: (() => void) | null } = { cb: null };

  const makeEl = (tag: string, fixedWidth?: number): NodeLike => {
    const attrs = new Map<string, string>();
    const el: NodeLike = {
      tagName: tag.toUpperCase(),
      id: '',
      className: '',
      textContent: '',
      innerHTML: '',
      parentElement: null,
      parentNode: null,
      children: [],
      attrs,
      style: {},
      isConnected: true,
      classList: {
        contains: (cls: string) => el.className.split(/\s+/).includes(cls)
      },
      setAttribute(k: string, v: string) {
        attrs.set(k, v);
      },
      getAttribute(k: string) {
        return attrs.get(k) ?? null;
      },
      removeAttribute(k: string) {
        attrs.delete(k);
      },
      hasAttribute(k: string) {
        return attrs.has(k);
      },
      addEventListener() {},
      closest: () => null,
      contains(target: NodeLike) {
        return target === el || el.children.some((c) => c.contains(target));
      },
      querySelector(sel: string) {
        if (sel === '.ag-lm-body') return el._innerSpanBody ?? null;
        if (sel === '.ag-lm-state') return el._innerSpanState ?? null;
        if (sel === '.ag-lm-ring-btn') return el._innerSpanRing ?? null;
        if (sel === '.ag-live-metrics-text') return el._innerSpanText ?? null;
        return null;
      },
      querySelectorAll(sel: string) {
        const out: NodeLike[] = [];
        const walk = (node: NodeLike) => {
          if (sel === '[data-ag-row-mode]' && node.hasAttribute('data-ag-row-mode')) out.push(node);
          node.children.forEach(walk);
        };
        walk(el);
        return out;
      },
      insertBefore(child: NodeLike, ref: NodeLike | null) {
        if (child.parentElement) child.remove();
        const idx = ref ? el.children.indexOf(ref) : -1;
        if (idx >= 0) el.children.splice(idx, 0, child);
        else el.children.push(child);
        child.parentElement = el;
        child.parentNode = el;
        child.isConnected = true;
        return child;
      },
      appendChild(child: NodeLike) {
        return el.insertBefore(child, null);
      },
      remove() {
        if (el.parentElement) {
          const idx = el.parentElement.children.indexOf(el);
          if (idx >= 0) el.parentElement.children.splice(idx, 1);
        }
        el.parentElement = null;
        el.parentNode = null;
        el.isConnected = false;
      },
      cloneNode() {
        const copy = makeEl(el.tagName);
        copy.className = el.className;
        el.attrs.forEach((v, k) => copy.attrs.set(k, v));
        copy.getBoundingClientRect = () => {
          // Simulates CSS rule: [data-ag-row-mode="bottom"] { width: calc(100% - 8px) !important; }
          const hasBottomCssWidth =
            copy.getAttribute('data-ag-row-mode') === 'bottom' &&
            !String(copy.style.cssText || '').includes('width:auto');
          const fit = copy.getAttribute('data-ag-fit') || 'full';
          return { width: hasBottomCssWidth ? 1432 : (fitIntrinsicWidths[fit] ?? 240), height: 22, top: 0, left: 0 };
        };
        return copy;
      },
      getBoundingClientRect: () => ({ width: fixedWidth ?? 0, height: 24, top: 0, left: 0 }),
      get nextElementSibling() {
        if (!el.parentElement) return null;
        const idx = el.parentElement.children.indexOf(el);
        return idx >= 0 ? el.parentElement.children[idx + 1] ?? null : null;
      },
      get nextSibling() {
        return el.nextElementSibling;
      }
    };
    Object.defineProperty(el, 'id', {
      get: () => el.attrs.get('id') || '',
      set: (v: string) => {
        const prev = el.attrs.get('id');
        if (prev) elementsById.delete(prev);
        if (v) {
          el.attrs.set('id', v);
          elementsById.set(v, el);
        } else {
          el.attrs.delete('id');
        }
      }
    });
    return el;
  };

  const leftChild = makeEl('button', 170);
  const rightChild = makeEl('button', 140);
  const leftGroup = makeEl('div');
  leftGroup.appendChild(leftChild);
  const rightGroup = makeEl('div');
  rightGroup.appendChild(rightChild);

  const row = makeEl('div');
  row.className = 'agy-composer-footer';
  row.appendChild(leftGroup);
  row.appendChild(rightGroup);
  row.getBoundingClientRect = () => ({ width: rowWidth, height: 28, top: 500, left: 0 });

  const inputBox = makeEl('div');
  inputBox.id = 'antigravity.agentSidePanelInputBox';
  inputBox.appendChild(row);
  inputBox.getBoundingClientRect = () => ({ width: rowWidth, height: 80, top: 460, left: 0 });
  const modelTrigger = leftChild;
  modelTrigger.closest = (sel: string) => (sel === '.agy-composer-footer' ? row : null);
  inputBox.querySelector = (sel: string) => (sel.includes('model-selector-trigger') ? modelTrigger : null);

  const cid = '11111111-2222-3333-4444-555555555555';
  const sandbox = createSandbox({
    __AG_BOOT_PAYLOAD__: boot,
    location: { pathname: `/c/${cid}`, href: `http://127.0.0.1/c/${cid}`, search: '' },
    getComputedStyle: () => ({ display: 'flex', flexDirection: 'row', columnGap: '0px', paddingLeft: '0px', paddingRight: '0px', flexGrow: '0' }),
    requestAnimationFrame: (cb: () => void) => {
      cb();
      return 1;
    },
    cancelAnimationFrame: () => {},
    ResizeObserver: class {
      constructor(cb: () => void) {
        roState.cb = cb;
      }
      observe() {}
      disconnect() {}
    }
  });

  const bodyEl = makeEl('body');
  sandbox.document.body = bodyEl;
  const origCreate = sandbox.document.createElement.bind(sandbox.document);
  sandbox.document.createElement = (tag: string) => {
    if (tag.toLowerCase() === 'div') {
      const el = makeEl('div');
      el._innerSpanBody = makeEl('span');
      el._innerSpanState = makeEl('span');
      el._innerSpanRing = makeEl('span');
      el._innerSpanText = makeEl('span');
      return el;
    }
    return origCreate(tag);
  };
  sandbox.document.querySelectorAll = (sel: string) =>
    sel.includes('antigravity.agentSidePanelInputBox') ? [inputBox] : [];
  sandbox.document.getElementById = (id: string) =>
    elementsById.get(id) || sandbox.headChildren.find((el: { id: string }) => el.id === id) || null;

  const code = parts.map(readRuntimePart).join('\n');
  vm.runInContext(`const __AG_BOOT_PAYLOAD__ = globalThis.__AG_BOOT_PAYLOAD__;\n${code}`, sandbox);

  const makePayload = (hit: number) => ({
    cid,
    hasData: true,
    view: {
      cacheMode: 'session',
      last: { ttft: 1.2, tps: 97, hit, out: 1500 },
      session: { hit, ttftMed: 1.1 },
      ctx: { used: 76000, limit: 512000, pct: 15, level: 'ok', cached: 60000, fresh: 16000 }
    }
  });

  // 1. Narrow row forces pill into bottom mode.
  sandbox.__ag_render_metrics(makePayload(86));
  const pill = elementsById.get('ag-live-metrics-pill');
  expect(pill?.getAttribute('data-ag-row-mode')).toBe('bottom');

  // 2. Metrics update while in bottom mode triggers probe re-measurement.
  sandbox.__ag_render_metrics(makePayload(87));

  // 3. Widening the composer should restore pill back to middle/full instead of staying latched at bottom/min.
  rowWidth = 760;
  expect(roState.cb).not.toBeNull();
  roState.cb!();
  expect(pill?.getAttribute('data-ag-row-mode')).toBe('middle');
  expect(pill?.getAttribute('data-ag-fit')).toBe('full');
  expect(pill?.parentElement).toBe(row);
});
