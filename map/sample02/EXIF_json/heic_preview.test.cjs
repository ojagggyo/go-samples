const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(__dirname + '/main.0.4.0.go', 'utf8');
const script = source.slice(source.indexOf('let heicLibrary;'), source.indexOf('async function refresh()'));

test('complete page JavaScript parses', () => {
  new vm.Script(source.slice(source.lastIndexOf('<script>') + '<script>'.length, source.lastIndexOf('</script>')));
});

test('multiple selections retain adjusted coordinates across pages and save together', async () => {
  const elements = new Map();
  const get = id => {
    if (!elements.has(id)) elements.set(id, { value: 'unlocated', scrollIntoView() {} });
    return elements.get(id);
  };
  const requests = [];
  const moves = [];
  const mapEvents = {};
  const context = vm.createContext({
    document: { getElementById: get },
    map: { on(event, handler) { mapEvents[event] = handler; }, removeLayer() {}, setView(...args) { moves.push(args); } },
    L: { divIcon: x => x, marker: () => ({ addTo() { return this; }, on() {}, setLatLng() {}, dragging: { disable() {}, enable() {} } }) },
    fetch: async (url, options) => { requests.push({url, options}); return {ok:true}; },
    requestNo: 0,
  });
  vm.runInContext(source.slice(source.indexOf('let unlocatedOffset ='), source.indexOf("map.on('moveend', refresh)")), context);
  context.loadUnlocated = async () => {};
  mapEvents.click({latlng:{lat:38.9088661,lng:140.8097197}});
  assert.equal(get('coordinate-input').value, '38.9088661,140.8097197');
  assert.equal(requests.length, 0);
  context.selectUnlocatedPhoto({ id: -1, name: 'photo.jpg', suggestion: {name:'nearby.jpg',lat:35,lng:139,differenceSeconds:300} });
  assert.equal(requests.length, 0);
  get('use-suggestion').onclick();
  assert.equal(get('coordinate-input').value, '35,139');
  assert.equal(requests.length, 0);
  const submitCoordinates = value => {
    get('coordinate-input').value = value;
    get('coordinate-form').onsubmit({ preventDefault() {} });
  };
  const beforeInvalid = moves.length;
  for (const invalid of ['91,140', '38,181', ',140', 'NaN,140', '38,140,1', '38']) submitCoordinates(invalid);
  assert.equal(moves.length, beforeInvalid);
  submitCoordinates(' 38.9088661,140.8097197 ');
  assert.deepEqual(Array.from(moves.at(-1)[0]), [38.9088661,140.8097197]);
  assert.equal(requests.length, 0);
  vm.runInContext("unlocatedPage = [{id:-2,name:'second.jpg'}, {id:-3,name:'third.jpg'}]", context);
  get('select-page').onclick();
  assert.equal(get('selection-count').textContent, '3枚選択');
  context.selectUnlocatedPhoto({id:-3,name:'third.jpg'});
  assert.equal(get('selection-count').textContent, '2枚選択');
  vm.runInContext('unlocatedOffset = 30', context);
  get('filter-year').value = 'unknown';
  get('filter-month').value = '12';
  get('filter-year').onchange();
  assert.equal(get('filter-month').value, '');
  assert.equal(get('filter-month').disabled, true);
  assert.equal(vm.runInContext('unlocatedOffset', context), 0);
  assert.equal(get('selection-count').textContent, '2枚選択');
  await get('save-location').onclick();
  assert.equal(requests.length, 1);
  assert.equal(requests[0].url, './api/location');
  assert.deepEqual(JSON.parse(requests[0].options.body), {ids:[-1,-2],lat:38.9088661,lng:140.8097197});
  assert.match(get('location-result').textContent, /保存しました/);
  assert.equal(get('selection-count').textContent, '0枚選択');
  submitCoordinates('35，139');
  assert.deepEqual(Array.from(moves.at(-1)[0]), [35,139]);
  assert.equal(requests.length, 1);
});

function setup(convert, input = new Blob(['heic test data'])) {
  let fetches = 0;
  const revoked = [];
  const element = () => ({ children: [], replaceChildren(...children) { this.children = children; } });
  const context = vm.createContext({
    markers: new Map(),
    visibleMediaIDs: new Set(),
    layer: { removeLayer() {} },
    window: { HeicTo: convert },
    document: { createElement: element, head: { append(script) { script.onload(); } } },
    fetch: async () => { fetches++; return { ok: true, blob: async () => input }; },
    URL: { createObjectURL: () => 'blob:preview', revokeObjectURL: url => revoked.push(url) },
    console: { error() {} },
  });
  vm.runInContext(script, context);
  return { context, revoked, fetches: () => fetches };
}

test('JPEG and PNG with HEIC extensions display without loading the converter', async () => {
  for (const [type, bytes] of [
    ['image/jpeg', [255, 216, 255, 224, 0, 16]],
    ['image/png', [137, 80, 78, 71, 13, 10, 26, 10]],
  ]) {
    const input = new Blob([new Uint8Array(bytes)], { type: 'image/heic' });
    const state = setup(() => { throw Error('converter must not run'); }, input);
    state.context.document.head.append = () => { throw Error('library must not load'); };
    const result = await state.context.heicPreview('/media?id=1');
    assert.equal(result.type, type);
    assert.deepEqual(await result.arrayBuffer(), await input.arrayBuffer());
  }
});

test('sidebar thumbnails load when visible and release previews on cleanup', async () => {
  const state = setup(async () => ({}));
  let visible;
  state.context.document.getElementById = () => ({});
  state.context.IntersectionObserver = class {
    constructor(callback) { visible = callback; }
    observe() {}
    disconnect() {}
  };
  let opened = 0;
  const button = state.context.heicThumbnail({ name: 'photo.heic' }, '/media?id=1', () => opened++);
  assert.equal(state.fetches(), 0);
  visible([{ isIntersecting: true }]);
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(button.children[0].src, 'blob:preview');
  button.onclick();
  assert.equal(opened, 1);
  vm.runInContext('thumbnailCleanups.splice(0).forEach(cleanup => cleanup())', state.context);
  assert.deepEqual(state.revoked, ['blob:preview']);
});

test('map refresh preserves open previews even when sampling excludes them', () => {
  const { context } = setup(async () => ({}));
  let open = true;
  const selected = { isPopupOpen: () => false };
  const preview = { isPopupOpen: () => open };
  const obsolete = { isPopupOpen: () => false };
  const removed = [];
  context.layer.removeLayer = marker => removed.push(marker);
  context.markers.set(1, selected);
  context.markers.set(2, preview);
  context.markers.set(3, obsolete);
  context.reconcileMarkers([{ id: 1 }]);
  assert.equal(context.markers.get(1), selected);
  assert.equal(context.markers.get(2), preview);
  assert.deepEqual(removed, [obsolete]);
  // 再度の検索でも開いているポップアップを破棄しない。
  context.reconcileMarkers([{ id: 1 }]);
  assert.equal(context.markers.get(2), preview);
  open = false;
  context.reconcileMarkers([{ id: 1 }]);
  assert.equal(context.markers.has(2), false);
});

test('preview requests share conversion and failed requests can retry', async () => {
  let fail = true;
  const state = setup(async () => { if (fail) throw Error('decode'); return { type: 'image/jpeg' }; });
  const a = state.context.heicPreview('/media?id=1');
  assert.equal(a, state.context.heicPreview('/media?id=1'));
  await assert.rejects(a);
  fail = false;
  const result = await state.context.heicPreview('/media?id=1');
  assert.equal(result.type, 'image/jpeg');
  await state.context.heicPreview('/media?id=1');
  assert.equal(state.fetches(), 2);
});

test('different HEIC files convert serially', async () => {
  let running = 0, peak = 0;
  const state = setup(async () => {
    peak = Math.max(peak, ++running);
    await new Promise(resolve => setImmediate(resolve));
    running--;
    return {};
  });
  await Promise.all([1, 2, 3].map(id => state.context.heicPreview('/media?id=' + id)));
  assert.equal(peak, 1);
});

test('closed popups ignore late results and open previews release object URLs', async () => {
  let finish;
  const state = setup(() => new Promise(resolve => { finish = resolve; }));
  const handlers = {};
  let content, open = true;
  const marker = {
    bindPopup(value) { content = value; },
    on(event, callback) { handlers[event] = callback; },
    isPopupOpen: () => open,
    getPopup: () => ({ update() {} }),
  };
  state.context.bindHeicPopup(marker, { name: 'test.heic' }, '/media?id=1');
  const pending = handlers.popupopen();
  await new Promise(resolve => setImmediate(resolve));
  open = false;
  handlers.popupclose();
  finish({});
  await pending;
  assert.equal(content.children.length, 0);
  open = true;
  await handlers.popupopen();
  assert.equal(content.children[0].src, 'blob:preview');
  handlers.popupclose();
  assert.deepEqual(state.revoked, ['blob:preview']);
});
