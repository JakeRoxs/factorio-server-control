const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {act, create} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

test("server cards target their own lifecycle endpoints and load profile definitions", async t => {
    const calls = [];
    const client = {
        get: async path => {
            calls.push(["get", path]);
            return {data: path === "/api/profiles" ? {profiles: [{id: "preset", name: "Reusable preset", is_global: true}]} : [
                {id: "2", name: "Second", bindip: "127.0.0.1", port: 34198, running: false},
                {id: "3", name: "Third", port: 34199, running: true}
            ]};
        },
        post: async (path, data) => { calls.push(["post", path, data]); return {data: true}; }
    };
    const Button = props => React.createElement("button", props, props.children);
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": loadModule("api/resources/servers.js", {"../client": client}).default,
        "../../api/resources/profiles": loadModule("api/resources/profiles.js", {"../client": client}).default,
        "../../api/resources/server": {start() { throw new Error("default-server API used"); }, stop() { throw new Error("default-server API used"); }},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": Button,
        "../components/Alert": () => null
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => { root = create(React.createElement(Servers, {canManage: true})); });
    t.after(() => act(() => root.unmount()));
    const button = text => root.root.findAllByType(Button).find(node => React.Children.toArray(node.props.children).some(child => typeof child === "string" && child.trim() === text));
    await act(async () => { await button("Start").props.onClick(); });
    await act(async () => { await button("Stop").props.onClick(); });
    assert.deepEqual(calls.filter(call => call[0] === "post"), [
        ["post", "/api/servers/2/start", {bindip: "127.0.0.1", port: 34198, savefile: "Load Latest"}],
        ["post", "/api/servers/3/stop", undefined]
    ]);
    await act(async () => { button("Global Profiles").props.onClick(); });
    assert.ok(calls.some(call => call[1] === "/api/profiles"));
    assert.match(JSON.stringify(root.toJSON()), /Reusable preset/);
});

test("server edit exposes a submit button and saves mimalloc", async t => {
    const updates = [];
    let useMimalloc = false;
    const Button = loadModule("App/components/Button.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {}
    }).default;
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => [{id: "1", name: "Server", version: "stable", base_mod_version: "2.1.20", port: 34197, use_mimalloc: useMimalloc}],
            update: async (...args) => { updates.push(args); useMimalloc = args[1].use_mimalloc; }
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": Button,
        "../components/Alert": () => null
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => { root = create(React.createElement(Servers, {canManage: true, serverStatus: {id: "1", name: "Stale server name", base_mod_version: "2.0.77", known: true, running: true, use_mimalloc: false}})); });
    t.after(() => act(() => root.unmount()));
    assert.match(JSON.stringify(root.toJSON()), /2\.1\.20/);
    assert.doesNotMatch(JSON.stringify(root.toJSON()), /Stale server name/);
    const edit = root.root.findAllByType(Button).find(node => React.Children.toArray(node.props.children).some(child => typeof child === "string" && child.includes("Edit")));
    act(() => edit.props.onClick());
    const checkbox = root.root.findByProps({name: "use_mimalloc"});
    act(() => checkbox.props.onChange({target: {name: "use_mimalloc", type: "checkbox", checked: true}}));
    assert.equal(root.root.findAllByType("button").filter(node => node.props.type === "submit").length, 1);
    await act(async () => { await root.root.findByType("form").props.onSubmit({preventDefault() {}}); });
    assert.equal(updates[0][0], "1");
    assert.equal(updates[0][1].use_mimalloc, true);
    assert.deepEqual(Object.keys(updates[0][1]).sort(), ["name", "use_mimalloc"]);
    const editAgain = root.root.findAllByType(Button).find(node => React.Children.toArray(node.props.children).some(child => typeof child === "string" && child.includes("Edit")));
    act(() => editAgain.props.onClick());
    assert.equal(root.root.findByProps({name: "use_mimalloc"}).props.checked, true);
});

test("Servers shows the existing runtime and follows shared live status without another subscription", async t => {
    const subscriptions = [];
    const Panel = loadModule("App/components/Panel.jsx", {
        react: React,
        "./HelpTip": () => null
    }).default;
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {list: async () => [{id: "1", name: "Existing server", running: false, stopping: false, port: 34197}]},
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../../api/socket": {emit: (...args) => subscriptions.push(args), on() {}, off() {}},
        "../components/PageHeader": () => null,
        "../components/Panel": Panel,
        "../components/Button": props => React.createElement("button", {disabled: props.isDisabled}, props.children),
        "../components/Alert": () => null
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => {
        root = create(React.createElement(Servers, {canManage: true, serverStatus: {id: "1", running: true, stopping: false, known: true}}));
    });
    t.after(() => act(() => root.unmount()));
    assert.equal(root.root.findAllByType("h3").filter(node => node.children.includes("Existing server")).length, 1);
    assert.match(JSON.stringify(root.toJSON()), /Running/);
    assert.doesNotMatch(JSON.stringify(root.toJSON()), /Stopped/);
    assert.deepEqual(subscriptions, []);
    act(() => root.update(React.createElement(Servers, {canManage: true, serverStatus: {id: "1", running: false, stopping: false, known: true}})));
    assert.match(JSON.stringify(root.toJSON()), /Stopped/);
    assert.doesNotMatch(JSON.stringify(root.toJSON()), /Running/);
});
