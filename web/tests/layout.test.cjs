const assert = require('node:assert/strict');
const { test } = require('node:test');
const path = require('node:path');
const Module = require('node:module');
const { buildSync } = require('esbuild');

function loadSource(relative) {
  const filename = path.resolve(__dirname, '..', relative);
  const result = buildSync({
    entryPoints: [filename],
    tsconfig: path.resolve(__dirname, '../tsconfig.json'),
    bundle: true,
    write: false,
    platform: 'node',
    format: 'cjs',
  });
  const compiled = new Module(filename, module);
  compiled.filename = filename;
  compiled.paths = module.paths;
  compiled._compile(result.outputFiles[0].text, filename);
  return compiled.exports;
}

const { restoreSettings } = loadSource('src/store/modules/app/settings.ts');
const { findPrimaryName, findFirstLeaf } = loadSource('src/components/menu/menu-tree.ts');

test('old saved settings receive layout defaults without losing preferences', () => {
  const settings = restoreSettings(JSON.stringify({ menuWidth: 260, footer: false, theme: 'dark' }));
  assert.equal(settings.layout, 'left');
  assert.equal(settings.tabMode, 'googlecard');
  assert.equal(settings.menuWidth, 260);
  assert.equal(settings.footer, false);
  assert.equal(settings.theme, 'dark');
});

test('legacy top menu migrates and a saved layout takes precedence', () => {
  assert.equal(restoreSettings('{"topMenu":true}').layout, 'top');
  const columns = restoreSettings('{"layout":"columns","topMenu":true,"menuCollapse":true,"tabMode":"rounded"}');
  assert.equal(columns.topMenu, false);
  assert.equal(columns.menuCollapse, false);
  assert.equal(columns.tabMode, 'rounded');
});

test('invalid preferences cannot prevent startup or create unsupported tabs', () => {
  for (const input of [null, '{broken', 'null', '[]', '42', '{"layout":"missing","tabMode":"missing"}']) {
    const settings = restoreSettings(input);
    assert.equal(settings.layout, 'left');
    assert.equal(settings.tabMode, 'googlecard');
  }
});

test('refresh loads current server menus even when old settings contain route state', () => {
  const settings = restoreSettings(JSON.stringify({
    menuCollapse: true,
    theme: 'dark',
    serverMenu: [{ name: 'old-menu' }],
    serverRoute: [{ name: 'old-menu', path: '/old-menu' }],
    isDynamicAddedRoute: true,
  }));
  assert.equal(settings.menuCollapse, true);
  assert.equal(settings.theme, 'dark');
  assert.deepEqual(settings.serverMenu, []);
  assert.deepEqual(settings.serverRoute, []);
  assert.equal(settings.isDynamicAddedRoute, false);
});

const leaf = (name, extra = {}) => ({ name, path: `/${name}`, meta: {}, ...extra });
const tree = [
  leaf('home'),
  leaf('system', { children: [
    leaf('hidden', { meta: { hideInMenu: true } }),
    leaf('organization', { children: [leaf('roles')] }),
  ] }),
];

test('nested routes select their primary menu and unknown routes do not', () => {
  assert.equal(findPrimaryName(tree, 'roles'), 'system');
  assert.equal(findPrimaryName(tree, 'system'), 'system');
  assert.equal(findPrimaryName(tree, 'home'), 'home');
  assert.equal(findPrimaryName(tree, 'missing'), null);
  assert.equal(findPrimaryName([], 'roles'), null);
});

test('primary navigation skips hidden leaves and handles empty groups', () => {
  assert.equal(findFirstLeaf(tree[1]).name, 'roles');
  assert.equal(findFirstLeaf(tree[0]).name, 'home');
  assert.equal(findFirstLeaf(leaf('empty', { children: [leaf('hidden', { meta: { hideInMenu: true } })] })), null);
});

test('unmounting one route subscriber preserves other layout subscribers', () => {
  const { listenerRouteChange, setRouteEmitter, removeRouteListener } = loadSource('src/utils/route-listener.ts');
  const seen = [];
  const stopFirst = listenerRouteChange(() => seen.push('first'));
  const stopSecond = listenerRouteChange(() => seen.push('second'));
  setRouteEmitter({ name: 'home' });
  stopFirst();
  setRouteEmitter({ name: 'roles' });
  assert.deepEqual(seen, ['first', 'second', 'second']);
  stopSecond();
  removeRouteListener();
  let stale = false;
  listenerRouteChange(() => { stale = true; });
  assert.equal(stale, false);
});
