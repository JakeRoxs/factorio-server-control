const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {create, act} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

test("process strip starts the selected server with its own startup configuration", async t => {
    const calls = [];
    const state = {
        selectedServer: {id: "2", name: "Second factory", savefile: "second.zip", bindip: "127.0.0.2", port: 34200, base_mod_version: "2.1.22"},
        refreshServers: async () => {}
    };
    const Button = props => React.createElement("button", {onClick: props.onClick, disabled: props.isDisabled}, props.children);
    const Bar = loadModule("App/components/ProfileContextBar.jsx", {
        react: React,
        "react-router-dom": {useLocation: () => ({pathname: "/mods"})},
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "../../api/resources/server": {start: async (...args) => calls.push(args)},
        "./Button": Button,
        "./ConfirmDialog": () => null,
        "../context/ServerContext": {useServers: () => state},
        "../context/ProfileContext": {useProfiles: () => ({state: {profiles: []}})}
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => { root = create(React.createElement(Bar, {canManage: true, serverStatus: {known: true, running: false}, refreshServerStatus: async () => {}})); });
    t.after(() => act(() => root.unmount()));
    assert.match(JSON.stringify(root.toJSON()), /Second factory/);
    assert.doesNotMatch(JSON.stringify(root.toJSON()), /Server 1/);
    const start = root.root.findAllByType(Button).find(button => button.findAllByType("span").some(span => span.children.includes("Start server")));
    assert.equal(start.props["aria-label"], "Start server");
    await act(async () => start.props.onClick());
    assert.deepEqual(calls, [["127.0.0.2", 34200, "second.zip"]]);
    act(() => root.update(React.createElement(Bar, {canManage: false, serverStatus: {known: true, running: false}})));
    assert.equal(root.root.findAllByType(Button).length, 0);
    assert.match(JSON.stringify(root.toJSON()), /Read-only access/);
});

test("shared buttons retain accessible names when responsive styles hide their text", t => {
    const Button = loadModule("App/components/Button.jsx", {
        react: React, "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null}, "@fortawesome/free-solid-svg-icons": {}
    }).default;
    let root;
    act(() => { root = create(React.createElement(Button, {"aria-label": "Save & stop"}, React.createElement("span", {className: "ui-context-action-label"}, "Save & stop"))); });
    t.after(() => act(() => root.unmount()));
    assert.equal(root.root.findByType("button").props["aria-label"], "Save & stop");
});

test("navigation switches running servers for viewers without profile activation", async t => {
    const selections = [];
    const state = {
        serverID: "2", selectedServer: {id: "2", name: "Second factory"},
        servers: [{id: "1", name: "First"}, {id: "2", name: "Second factory"}],
        selectServer: id => selections.push(id)
    };
    const Link = props => React.createElement("a", {href: props.to}, props.children);
    const Layout = loadModule("App/components/Layout.jsx", {
        react: React,
        "react-router-dom": {Link, NavLink: Link, Outlet: () => null, useLocation: () => ({pathname: "/mods"})},
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null},
        "@fortawesome/free-solid-svg-icons": {},
        "./Button": () => null,
        "./ProfileContextBar": () => null,
        "../context/ServerContext": {useServers: () => state},
        "./BrandMark": () => null,
        "./overlay": {focusableElements: () => [], lockBodyScroll: () => () => {}, trapFocus() {}},
        "./RouteContent": props => props.children
    }, {
        window: {matchMedia: () => ({matches: false, addEventListener() {}, removeEventListener() {}}), scrollTo() {}},
        __FSM_UI_REVISION__: "local", __FSM_UI_VERSION__: "0.20.1"
    }).default;
    let root;
    await act(async () => { root = create(React.createElement(Layout, {canManage: false, serverStatus: {running: true}})); });
    t.after(() => act(() => root.unmount()));
    const selector = root.root.findByType("select");
    assert.equal(selector.props.disabled, false);
    act(() => selector.props.onChange({target: {value: "1"}}));
    assert.deepEqual(selections, ["1"]);
    const internal = root.root.findAllByType("a").filter(link => link.props.href?.startsWith("/"));
    assert.ok(internal.length > 5);
    assert.ok(internal.every(link => link.props.href.includes("server=2")));
    act(() => root.root.findAllByType(Link).find(link => link.props.onClick).props.onClick());
    assert.equal(root.root.findAllByProps({"aria-controls": "profile-switch-menu"}).length, 0);
});
