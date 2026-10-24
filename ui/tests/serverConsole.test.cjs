const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {create, act} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

test("console output and commands target the selected server, never the shared WebSocket", async t => {
    const calls = [];
    const Console = loadModule("App/views/Console.jsx", {
        react: React,
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null}, "@fortawesome/free-solid-svg-icons": {},
        "../../api/client": {post: async (...args) => {calls.push(["command", ...args]); return {data: "Second server response"};}},
        "../../api/resources/log": {tailForServer: async id => {calls.push(["logs", id]); return ["Second server log"]; }},
        "../context/ServerContext": {useServers: () => ({serverID: "2"})},
        "../components/PageHeader": () => null,
        "../components/Panel": props => props.content,
        "../components/Button": props => React.createElement("button", {disabled: props.isDisabled}, props.children),
        "../components/Alert": props => props.children,
        "../components/LogViewport": props => React.createElement("pre", null, props.lines.join("\n")),
        "../components/ScopeBadge": () => null
    }, {window: {setInterval: () => 1, clearInterval() {}}}).default;
    let root;
    await act(async () => { root = create(React.createElement(Console, {serverStatus: {running: true, stopping: false}})); });
    t.after(() => act(() => root.unmount()));
    assert.match(JSON.stringify(root.toJSON()), /Second server log/);
    act(() => root.root.findByType("input").props.onChange({target: {value: "/players online"}}));
    await act(async () => root.root.findByType("form").props.onSubmit({preventDefault() {}}));
    assert.deepEqual(calls, [["logs", "2"], ["command", "/api/servers/2/command", {command: "/players online"}]]);
    assert.match(JSON.stringify(root.toJSON()), /Second server response/);
    act(() => root.update(React.createElement(Console, {serverStatus: {known: false, running: false}})));
    assert.equal(root.root.findByType("input").props.disabled, true);
});
