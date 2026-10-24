const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {create, act} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

test("switching servers changes navigation, not profiles, and discards late status responses", async t => {
    let location = {pathname: "/mods", search: "?server=2"};
    const navigations = [], requests = [], timers = new Set();
    let state;
    const {ServerProvider, useServers} = loadModule("App/context/ServerContext.jsx", {
        react: React,
        "react-router-dom": {useLocation: () => location, useNavigate: () => value => navigations.push(value)},
        "../../api/resources/servers": {list: async () => [{id: "1", name: "First"}, {id: "2", name: "Second"}]},
        "../../api/client": {get: url => new Promise(resolve => requests.push({url, resolve}))}
    }, {window: {setInterval: callback => {timers.add(callback); return callback;}, clearInterval: timer => timers.delete(timer)}});
    const Probe = () => { state = useServers(); return null; };
    let root;
    await act(async () => { root = create(React.createElement(ServerProvider, null, React.createElement(Probe))); });
    t.after(() => act(() => root.unmount()));
    assert.equal(state.selectedServer.name, "Second");
    assert.equal(state.serverStatus.known, false);
    assert.equal(requests[0].url, "/api/servers/2/status");
    act(() => state.selectServer("1"));
    assert.deepEqual(navigations, [{pathname: "/mods", search: "server=1"}]);
    location = {pathname: "/mods", search: "?server=1"};
    await act(async () => root.update(React.createElement(ServerProvider, null, React.createElement(Probe))));
    assert.equal(state.selectedServer.name, "First");
    await act(async () => requests[1].resolve({data: {running: false, stopping: false}}));
    assert.equal(state.serverStatus.id, "1");
    await act(async () => requests[0].resolve({data: {running: true, stopping: false}}));
    assert.equal(state.serverStatus.id, "1");
    assert.equal(state.serverStatus.running, false);
    assert.equal(timers.size, 1);
});

test("unknown server IDs remain unavailable instead of silently selecting Server 1", async t => {
    let state;
    const {ServerProvider, useServers} = loadModule("App/context/ServerContext.jsx", {
        react: React,
        "react-router-dom": {useLocation: () => ({pathname: "/", search: "?server=missing"}), useNavigate: () => () => assert.fail("Unexpected fallback")},
        "../../api/resources/servers": {list: async () => [{id: "1", name: "First"}]},
        "../../api/client": {get: async () => { throw new Error("Not found"); }}
    }, {window: {setInterval: () => 1, clearInterval() {}}});
    const Probe = () => { state = useServers(); return null; };
    let root;
    await act(async () => { root = create(React.createElement(ServerProvider, null, React.createElement(Probe))); });
    t.after(() => act(() => root.unmount()));
    assert.equal(state.serverID, "missing");
    assert.equal(state.selectedServer, null);
    assert.equal(state.serverStatus.known, false);
});
