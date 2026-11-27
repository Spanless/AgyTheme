import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import vm from 'node:vm';

export const ROOT_DIR = path.resolve(import.meta.dir, '..');
export const RUNTIME_DIR = path.join(ROOT_DIR, 'core', 'runtime');
export const STYLES_DIR = path.join(ROOT_DIR, 'core', 'styles');

export const readRuntimePart = (name: string): string => fs.readFileSync(path.join(RUNTIME_DIR, name), 'utf8');

export const sortedFiles = (dir: string, ext: string): string[] =>
  fs.readdirSync(dir).filter((f) => f.endsWith(ext)).sort();

export type Modules = {
  theme: boolean;
  i18n: boolean;
  metrics: boolean;
  deadlockHealer: boolean;
  zoomShortcuts: boolean;
};

type FakeEl = {
  tagName: string;
  id: string;
  textContent: string;
  parentNode: unknown;
  style: Record<string, unknown>;
  [k: string]: unknown;
};

export type Sandbox = Record<string, any> & {
  headChildren: FakeEl[];
  docAttrs: Map<string, string>;
};

export function createSandbox(extra: Record<string, unknown> = {}): Sandbox {
  const docAttrs = new Map<string, string>();
  const headChildren: FakeEl[] = [];
  const headEl = {
    appendChild(el: FakeEl) {
      el.parentNode = headEl;
      headChildren.push(el);
    }
  };
  const bodyEl = {
    classList: { contains: () => false },
    style: { removeProperty() {} }
  };
  const fakeDocument = {
    head: headEl,
    body: bodyEl,
    hidden: false,
    documentElement: {
      style: { colorScheme: 'light', removeProperty() {} },
      setAttribute: (k: string, v: string) => docAttrs.set(k, v),
      getAttribute: (k: string) => docAttrs.get(k) ?? null,
      removeAttribute: (k: string) => docAttrs.delete(k)
    },
    getElementById: (id: string) => headChildren.find((el) => el.id === id) || null,
    createElement(tag: string): FakeEl {
      const el: FakeEl = {
        tagName: tag.toUpperCase(),
        id: '',
        textContent: '',
        parentNode: null,
        style: {},
        setAttribute() {},
        getAttribute: () => null,
        removeAttribute() {},
        hasAttribute: () => false,
        addEventListener() {},
        querySelector: () => null,
        remove() {
          const idx = headChildren.indexOf(el);
          if (idx !== -1) headChildren.splice(idx, 1);
          el.parentNode = null;
        }
      };
      return el;
    },
    getElementsByTagName: (t: string) => (t === 'head' ? [headEl] : []),
    querySelector: () => null,
    querySelectorAll: () => [],
    createTreeWalker: () => ({ nextNode: () => false }),
    addEventListener() {},
    removeEventListener() {}
  };
  const localStorage = { getItem: () => null, setItem() {} };
  const sandbox: Sandbox = {
    location: { pathname: '/', href: 'http://127.0.0.1/', search: '' },
    navigator: { platform: 'MacIntel', onLine: true },
    history: { pushState() {}, replaceState() {} },
    addEventListener() {},
    removeEventListener() {},
    setInterval: () => 1,
    clearInterval() {},
    setTimeout: () => 1,
    clearTimeout() {},
    requestAnimationFrame: () => 1,
    document: fakeDocument,
    localStorage,
    Storage: { prototype: localStorage },
    NodeFilter: { SHOW_ELEMENT: 1, SHOW_TEXT: 4, FILTER_ACCEPT: 1, FILTER_REJECT: 2, FILTER_SKIP: 3 },
    MutationObserver: class {
      observe() {}
      disconnect() {}
    },
    headChildren,
    docAttrs,
    ...extra
  };
  sandbox.window = sandbox;
  sandbox.top = sandbox;
  vm.createContext(sandbox);
  return sandbox;
}

let builtBinary: string | null = null;

export function agythemeBinary(): string {
  if (process.env.AGYTHEME_BIN) return process.env.AGYTHEME_BIN;
  if (builtBinary) return builtBinary;
  const out = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'agytheme-bin-')), process.platform === 'win32' ? 'agytheme.exe' : 'agytheme');
  const res = Bun.spawnSync(['go', 'build', '-o', out, './cmd/agytheme'], { cwd: ROOT_DIR, stderr: 'pipe' });
  if (res.exitCode !== 0) throw new Error(`go build failed: ${res.stderr.toString()}`);
  builtBinary = out;
  return out;
}

export type BundleReport = {
  patchVersion: string;
  enabled: boolean;
  preset: string;
  channel: string;
  modules: Record<string, boolean>;
  script: string;
};

export function goBundle(opts: { state?: unknown; config?: unknown; safeMode?: boolean } = {}): BundleReport {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'agy-bundle-root-'));
  try {
    fs.mkdirSync(path.join(root, 'user'), { recursive: true });
    if (opts.state !== undefined) fs.writeFileSync(path.join(root, 'state.json'), JSON.stringify(opts.state));
    if (opts.config !== undefined) fs.writeFileSync(path.join(root, 'user', 'config.json'), JSON.stringify(opts.config));
    const args = [agythemeBinary(), 'bundle', '--json', '--root', root, '--core-dir', path.join(ROOT_DIR, 'core')];
    if (opts.safeMode) args.push('--safe-mode');
    const res = Bun.spawnSync(args, { stderr: 'pipe' });
    if (res.exitCode !== 0) throw new Error(`agytheme bundle failed: ${res.stderr.toString()}`);
    return JSON.parse(res.stdout.toString()) as BundleReport;
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
}
