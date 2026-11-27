import { expect, test } from 'bun:test';
import fs from 'node:fs';
import path from 'node:path';
import { ROOT_DIR, RUNTIME_DIR, STYLES_DIR, readRuntimePart, sortedFiles } from './helpers.ts';

test('runtime CSS and JS define the light/dark presets and status indicator', () => {
  const css = sortedFiles(STYLES_DIR, '.css')
    .map((f) => fs.readFileSync(path.join(STYLES_DIR, f), 'utf8'))
    .join('\n');
  const js = sortedFiles(RUNTIME_DIR, '.js').map(readRuntimePart).join('\n');
  for (const needle of ['#2B7FFF', '#181818', '#363636', '.ag-lm-ring-arc']) expect(css).toContain(needle);
  for (const needle of ['data-ag-preset', 'cascade-system-message-toolbar']) expect(js).toContain(needle);
  expect(js.includes('#agytheme-style') || js.includes("'agytheme-style'")).toBe(true);
});

test('core/i18n.json is valid JSON with substantial coverage and leaves model/effort labels untranslated', () => {
  const dict = JSON.parse(fs.readFileSync(path.join(ROOT_DIR, 'core', 'i18n.json'), 'utf8')) as Record<string, unknown>;
  expect(Object.keys(dict).length).toBeGreaterThanOrEqual(800);
  expect(typeof dict.Settings).toBe('string');
  for (const keepEn of ['Minimal', 'Low', 'Medium', 'High', 'Extra', 'Extra High', 'XHigh', 'Max', 'Orchestrator', 'Orchestrator (Carbon)', 'Orchestrator (Argon)']) {
    expect(Object.hasOwn(dict, keepEn)).toBe(false);
  }
});

test('README keeps canonical repository links and clean copy', () => {
  const readme = fs.readFileSync(path.join(ROOT_DIR, 'README.md'), 'utf8');
  expect(readme).toContain('https://github.com/Spanless/AgyTheme');
  expect(readme).not.toContain('<your-username>');
  expect(readme).not.toContain('状态灯');
  expect(readme).not.toContain('琥珀');
});
