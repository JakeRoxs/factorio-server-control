const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {create, act} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

test("profile presets capture a stopped source and apply only to the selected server", async t => {
    const calls = [];
    const servers = [{id: "1", name: "First", running: true}, {id: "2", name: "Second", running: false}];
    const Button = props => React.createElement("button", {onClick: props.onClick, disabled: props.isDisabled}, props.children);
    const Dialog = () => null;
    const Profiles = loadModule("App/views/Profiles.jsx", {
        react: React,
        "react-router-dom": {Link: props => React.createElement("a", {href: props.to}, props.children)},
        "../../api/resources/profiles": {list: async () => ({profiles: [{id: "preset", name: "Configured", is_global: true}, {id: "snapshot", name: "Old world"}]})},
        "../../api/resources/servers": {
            createProfile: async (...args) => calls.push(["capture", ...args]),
            applyGlobalProfile: async (...args) => calls.push(["apply", ...args])
        },
        "../context/ServerContext": {useServers: () => ({serverID: "2", servers, selectedServer: servers[1], serverStatus: {known: true, running: false}, refreshServers: async () => {}})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": Button,
        "../components/Alert": props => props.children,
        "../components/ConfirmDialog": Dialog,
        "./LegacyProfiles": () => { throw new Error("Server 2 must not expose Server 1 snapshot activation"); }
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => { root = create(React.createElement(Profiles, {canManage: true})); });
    t.after(() => act(() => root.unmount()));
    assert.doesNotMatch(JSON.stringify(root.toJSON()), /Old world/);
    act(() => root.root.findByProps({id: "preset-name"}).props.onChange({target: {value: "My setup"}}));
    await act(async () => root.root.findByType("form").props.onSubmit({preventDefault() {}}));
    assert.deepEqual(calls[0], ["capture", "2", "My setup", ""]);
    const apply = root.root.findAllByType(Button).find(node => React.Children.toArray(node.props.children).join("").includes("Apply to"));
    act(() => apply.props.onClick());
    await act(async () => root.root.findByType(Dialog).props.onSuccess());
    assert.deepEqual(calls[1], ["apply", "2", "preset"]);
    assert.ok(root.root.findAllByType("a").every(node => node.props.href.includes("server=2")));
});
