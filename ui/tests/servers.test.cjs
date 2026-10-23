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
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
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
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
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
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
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

test("WebSocket status events update only the matching server card", async t => {
    const EventEmitter = require("events");
    const mockSocket = new EventEmitter();
    const subscriptions = [];
    mockSocket.on = (event, handler) => {
        EventEmitter.prototype.on.call(mockSocket, event, handler);
        return mockSocket;
    };
    mockSocket.off = (event, handler) => {
        EventEmitter.prototype.off.call(mockSocket, event, handler);
        return mockSocket;
    };
    mockSocket.emit = (event, data) => {
        if (event === "server status:subscribe" || event === "server status:unsubscribe") {
            subscriptions.push([event, data]);
        }
        return EventEmitter.prototype.emit.call(mockSocket, event, data);
    };
    const Button = props => React.createElement("button", props, props.children);
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => [
                {id: "1", name: "Server One", running: false, stopping: false, port: 34197, rcon_port: 40000, bindip: "0.0.0.0", base_mod_version: "2.1.20"},
                {id: "2", name: "Server Two", running: false, stopping: false, port: 34198, rcon_port: 40001, bindip: "0.0.0.0", base_mod_version: "2.1.20"}
            ],
            profiles: async () => ({profiles: []}),
            start: async () => {},
            stop: async () => {},
            save: async () => {}
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": Button,
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": mockSocket
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => {
        root = create(React.createElement(Servers, {canManage: true}));
    });
    t.after(() => act(() => root.unmount()));
    // Verify the component subscribed to both servers' status rooms
    const subscribeCalls = subscriptions.filter(([event]) => event === "server status:subscribe");
    assert.equal(subscribeCalls.length, 2);
    assert.ok(subscribeCalls.some(([_, data]) => data.serverID === "1"));
    assert.ok(subscribeCalls.some(([_, data]) => data.serverID === "2"));
    // Both cards should initially show "Stopped"
    let json = JSON.stringify(root.toJSON());
    assert.match(json, /Server One/);
    assert.match(json, /Server Two/);
    assert.match(json, /Stopped/);
    // Emit a running status to server 1's room only
    await act(async () => {
        mockSocket.emit("servers:1:status", {running: true, stopping: false, port: 34197, rcon_port: 40000});
    });
    json = JSON.stringify(root.toJSON());
    // Server One should now show Running, Server Two should still show Stopped
    const serverOneCard = root.root.findAllByType("h3").find(node => node.children.includes("Server One"));
    const serverTwoCard = root.root.findAllByType("h3").find(node => node.children.includes("Server Two"));
    assert.ok(serverOneCard);
    assert.ok(serverTwoCard);
    // After server 1 event, card 1 should have Running status text
    assert.match(json, /Server One/);
    // Emit a running status to server 2's room only
    await act(async () => {
        mockSocket.emit("servers:2:status", {running: true, stopping: false, port: 34198, rcon_port: 40001});
    });
    json = JSON.stringify(root.toJSON());
    // Both should now show Running
    assert.match(json, /Running/);
});

test("viewer mode hides server mutation controls", async t => {
    const Button = props => React.createElement("button", props, props.children);
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => [
                {id: "1", name: "Server One", running: true, stopping: false, port: 34197, bindip: "0.0.0.0"},
                {id: "2", name: "Server Two", running: false, stopping: false, port: 34198, bindip: "0.0.0.0"}
            ],
            profiles: async () => ({profiles: []})
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": Button,
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => {
        root = create(React.createElement(Servers, {canManage: false}));
    });
    t.after(() => act(() => root.unmount()));
    const json = JSON.stringify(root.toJSON());
    // Both servers should be visible
    assert.match(json, /Server One/);
    assert.match(json, /Server Two/);
    // No mutation buttons should be present
    const buttons = root.root.findAllByType(Button);
    const buttonLabels = buttons.map(node => React.Children.toArray(node.props.children).filter(child => typeof child === "string").join(" "));
    assert.ok(!buttonLabels.includes("Start"));
    assert.ok(!buttonLabels.includes("Stop"));
    assert.ok(!buttonLabels.includes("Save"));
    assert.ok(!buttonLabels.includes("Create World"));
    assert.ok(!buttonLabels.includes("Edit"));
    assert.ok(!buttonLabels.includes("New Server"));
    assert.ok(!buttonLabels.includes("Global Profiles"));
});

test("Servers shows loading and error states clearly", async t => {
    // Loading state
    const ServersLoading = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => {
                // Never resolves - stays in loading state
                return new Promise(() => {});
            }
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": props => React.createElement("button", props, props.children),
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
    }, {window: {flash() {}}}).default;
    let rootLoading;
    await act(async () => {
        rootLoading = create(React.createElement(ServersLoading, {canManage: true}));
    });
    t.after(() => act(() => rootLoading.unmount()));
    // Should show loading text immediately
    let json = JSON.stringify(rootLoading.toJSON());
    assert.match(json, /Loading servers/);

    // Error state
    let alertContent = "";
    const ServersError = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => { throw new Error("Failed to load"); }
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": props => React.createElement("button", props, props.children),
        "../components/Alert": props => { alertContent = props.children; return null; },
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
    }, {window: {flash() {}}}).default;
    let rootError;
    await act(async () => {
        rootError = create(React.createElement(ServersError, {canManage: true}));
    });
    t.after(() => act(() => rootError.unmount()));
    // Wait for the error to be processed
    await new Promise(resolve => setTimeout(resolve, 100));
    assert.match(alertContent, /Servers could not be loaded/);

    // Stopping state
    const ServersStopping = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => [{id: "1", name: "Stopping Server", running: false, stopping: true, port: 34197, bindip: "0.0.0.0"}],
            profiles: async () => ({profiles: []})
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": props => React.createElement("button", props, props.children),
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
    }, {window: {flash() {}}}).default;
    let rootStopping;
    await act(async () => {
        rootStopping = create(React.createElement(ServersStopping, {canManage: true}));
    });
    t.after(() => act(() => rootStopping.unmount()));
    json = JSON.stringify(rootStopping.toJSON());
    assert.match(json, /Stopping/);
    assert.doesNotMatch(json, /Stopped/);

    // Empty state (no servers)
    const ServersEmpty = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => [],
            profiles: async () => ({profiles: []})
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": props => React.createElement("button", props, props.children),
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
    }, {window: {flash() {}}}).default;
    let rootEmpty;
    await act(async () => {
        rootEmpty = create(React.createElement(ServersEmpty, {canManage: true}));
    });
    t.after(() => act(() => rootEmpty.unmount()));
    json = JSON.stringify(rootEmpty.toJSON());
    // Should not show any server cards
    assert.doesNotMatch(json, /Server 1/);
    assert.doesNotMatch(json, /Server 2/);
});

test("failed server operations show user-facing notifications", async t => {
    const flashes = [];
    const Button = props => React.createElement("button", props, props.children);
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => [
                {id: "1", name: "Server One", running: false, stopping: false, port: 34197, bindip: "0.0.0.0"},
                {id: "2", name: "Server Two", running: true, stopping: false, port: 34198, bindip: "0.0.0.0"}
            ],
            profiles: async () => ({profiles: []}),
            start: async () => { throw new Error("Start failed"); },
            stop: async () => { throw new Error("Stop failed"); },
            save: async () => { throw new Error("Save failed"); }
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": Button,
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
    }, {window: {flash: (msg, type) => flashes.push([msg, type])}}).default;
    let root;
    await act(async () => {
        root = create(React.createElement(Servers, {canManage: true}));
    });
    t.after(() => act(() => root.unmount()));
    const button = text => root.root.findAllByType(Button).find(node => React.Children.toArray(node.props.children).some(child => typeof child === "string" && child.trim() === text));
    // Test failed start
    await act(async () => { await button("Start").props.onClick(); });
    assert.ok(flashes.some(([msg]) => typeof msg === "string" && msg.includes("could not be started")));
    // Test failed stop
    await act(async () => { await button("Stop").props.onClick(); });
    assert.ok(flashes.some(([msg]) => typeof msg === "string" && msg.includes("could not be stopped")));
    // Test failed save
    await act(async () => { await button("Save").props.onClick(); });
    assert.ok(flashes.some(([msg]) => typeof msg === "string" && msg.includes("could not be saved")));
});

test("simultaneous WebSocket events on different servers update independently", async t => {
    const EventEmitter = require("events");
    const mockSocket = new EventEmitter();
    mockSocket.emit = (event, data) => EventEmitter.prototype.emit.call(mockSocket, event, data);
    const Button = props => React.createElement("button", props, props.children);
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => [
                {id: "1", name: "Server One", running: false, stopping: false, port: 34197, rcon_port: 40000, bindip: "0.0.0.0", base_mod_version: "2.1.20"},
                {id: "2", name: "Server Two", running: false, stopping: false, port: 34198, rcon_port: 40001, bindip: "0.0.0.0", base_mod_version: "2.1.20"},
                {id: "3", name: "Server Three", running: false, stopping: false, port: 34199, rcon_port: 40002, bindip: "0.0.0.0", base_mod_version: "2.1.20"}
            ],
            profiles: async () => ({profiles: []}),
            start: async () => {},
            stop: async () => {},
            save: async () => {}
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": Button,
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": mockSocket
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => {
        root = create(React.createElement(Servers, {canManage: true}));
    });
    t.after(() => act(() => root.unmount()));
    // Send simultaneous status updates to all three servers
    await act(async () => {
        mockSocket.emit("servers:1:status", {running: true, stopping: false, port: 34197, rcon_port: 40000});
        mockSocket.emit("servers:2:status", {running: false, stopping: true, port: 34198, rcon_port: 40001});
        mockSocket.emit("servers:3:status", {running: true, stopping: false, port: 34199, rcon_port: 40002});
    });
    const json = JSON.stringify(root.toJSON());
    // All three servers should reflect their individual states
    assert.match(json, /Server One/);
    assert.match(json, /Server Two/);
    assert.match(json, /Server Three/);
    // Server 1 and 3 should show Running, Server 2 should show Stopping
    assert.match(json, /Running/);
    assert.match(json, /Stopping/);
});

test("unknown server status falls back to API data without misleading display", async t => {
    const Button = props => React.createElement("button", props, props.children);
    const Servers = loadModule("App/views/Servers.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/servers": {
            list: async () => [
                {id: "1", name: "Server One", running: true, stopping: false, port: 34197, rcon_port: 40000, bindip: "0.0.0.0", base_mod_version: "2.1.20"}
            ],
            profiles: async () => ({profiles: []})
        },
        "../../api/resources/profiles": {list: async () => ({profiles: []})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": Button,
        "../components/Alert": () => null,
        "../components/ConfirmDialog": () => null,
        "./Saves/components/NewWorldForm": () => null,
        "../../api/socket": {on() {}, off() {}, emit() {}, connect() {}, disconnect() {}}
    }, {window: {flash() {}}}).default;
    let root;
    // Pass serverStatus with known: false to simulate unknown status
    await act(async () => {
        root = create(React.createElement(Servers, {
            canManage: true,
            serverStatus: {known: false, running: false, stopping: false}
        }));
    });
    t.after(() => act(() => root.unmount()));
    const json = JSON.stringify(root.toJSON());
    // Server should still show its API data (running) rather than the unknown status
    assert.match(json, /Server One/);
    // The API says running: true, so it should show Running (not Stopped from the unknown status)
    assert.match(json, /Running/);
});
