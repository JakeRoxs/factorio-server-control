const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {create, act} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

async function setup(t, options = {}) {
    const calls = [], flashes = [], timers = new Set();
    let entries = options.entries || [
        {id: "1", name: "First", bindip: "0.0.0.0", port: 34197, running: false},
        {id: "2", name: "Second", bindip: "127.0.0.1", port: 34198, running: true}
    ];
    const Button = loadModule("App/components/Button.jsx", {
        react: React, "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null}, "@fortawesome/free-solid-svg-icons": {}
    }).default;
    const Dialog = () => null;
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null}, "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => { if (options.failList) throw new Error("Offline"); return entries; },
            start: async (...args) => { calls.push(["start", ...args]); if (options.failStart) throw new Error("Failed"); },
            stop: async (...args) => calls.push(["stop", ...args]),
            save: async (...args) => calls.push(["save", ...args]),
            update: async (...args) => calls.push(["update", ...args]),
            create: async (...args) => calls.push(["create", ...args]),
            delete: async (...args) => calls.push(["delete", ...args])
        },
        "../components/PageHeader": props => React.createElement("header", null, props.title, props.actions),
        "../components/Panel": props => React.createElement("section", null, props.content),
        "../components/Button": Button, "../components/Alert": props => React.createElement("div", null, props.children),
        "../components/ConfirmDialog": Dialog
    }, {window: {flash: (...args) => flashes.push(args), setInterval: callback => {timers.add(callback); return callback;}, clearInterval: timer => timers.delete(timer)}}).default;
    let root;
    const props = {canManage: options.canManage !== false, serverStatus: options.serverStatus || {known: false}};
    await act(async () => { root = create(React.createElement(Servers, props)); });
    t.after(() => act(() => root.unmount()));
    const button = name => root.root.findAllByType(Button).find(node => node.findByType("button").children.filter(child => typeof child === "string").join("").trim() === name);
    return {root, calls, flashes, timers, button, Button, Dialog, updateEntries: async next => {entries = next; await act(async () => { await [...timers][0](); });}};
}

test("server cards open scoped workspaces and target their own lifecycle endpoints", async t => {
    const app = await setup(t);
    const links = app.root.root.findAllByType("a").map(link => link.props.href);
    assert.ok(links.includes("/?server=2"));
    assert.ok(links.includes("/profiles?server=2"));
    await act(async () => app.button("Start").props.onClick());
    await act(async () => app.button("Stop").props.onClick());
    await act(async () => app.button("Save").props.onClick());
    assert.deepEqual(app.calls, [["start", "1", "0.0.0.0", 34197, "Load Latest"], ["stop", "2"], ["save", "2"]]);
});

test("server rename never writes allocator or startup settings", async t => {
    const app = await setup(t);
    act(() => app.button("Edit").props.onClick());
    const input = app.root.root.findByProps({name: "name"});
    act(() => input.props.onChange({target: {value: "Renamed"}}));
    assert.equal(app.root.root.findAllByProps({name: "use_mimalloc"}).length, 0);
    await act(async () => app.root.root.findByType("form").props.onSubmit({preventDefault() {}}));
    assert.deepEqual(app.calls, [["update", "1", {name: "Renamed"}]]);
});

test("viewer can open servers but sees no mutation controls", async t => {
    const app = await setup(t, {canManage: false});
    assert.equal(app.root.root.findAllByType("button").length, 0);
    assert.ok(app.root.root.findAllByType("a").length >= 2);
});

test("shared status only updates the matching card and retains catalog identity", async t => {
    const app = await setup(t, {serverStatus: {id: "1", name: "Stale name", known: true, running: true}});
    assert.doesNotMatch(JSON.stringify(app.root.toJSON()), /Stale name/);
    assert.equal(app.root.root.findAllByType("button").filter(node => node.children.includes(" Stop")).length, 2);
});

test("server-list polling refreshes independent runtime status", async t => {
    const app = await setup(t);
    await app.updateEntries([{id: "2", name: "Second", running: false, port: 34198}]);
    assert.equal(app.button("Stop"), undefined);
    assert.ok(app.button("Start"));
    assert.equal(app.timers.size, 1);
});

test("load and operation failures are visible", async t => {
    const unavailable = await setup(t, {failList: true});
    assert.match(JSON.stringify(unavailable.root.toJSON()), /Servers could not be loaded/);
    const app = await setup(t, {failStart: true});
    await act(async () => app.button("Start").props.onClick());
    assert.ok(app.flashes.some(([message, color]) => color === "red" && message.includes("could not start")));
});

test("creating a server submits explicit startup network values", async t => {
    const app = await setup(t);
    act(() => app.button("New server").props.onClick());
    for (const [name, value] of [["name", "Third"], ["version", "stable"], ["port", "34201"]]) {
        act(() => app.root.root.findByProps({name}).props.onChange({target: {value}}));
    }
    await act(async () => app.root.root.findByType("form").props.onSubmit({preventDefault() {}}));
    assert.deepEqual(app.calls, [["create", "Third", "stable", "0.0.0.0", 34201, false]]);
});
