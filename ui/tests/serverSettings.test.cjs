const {test} = require("node:test");
const assert = require("node:assert/strict");
const React = require("react");
const {act, create} = require("react-test-renderer");
const loadModule = require("./helpers/loadModule.cjs");

const Button = props => React.createElement("button", {disabled: props.isDisabled}, props.children);
const useForm = ({defaultValues = {}} = {}) => {
    const values = React.useRef(defaultValues);
    const reset = React.useCallback(next => { values.current = next; }, []);
    return {register: name => ({name}), handleSubmit: callback => submitted => callback(submitted), reset,
        watch: name => values.current[name], formState: {isDirty: true, errors: {}}};
};

async function settings(t, {id = "2", running = false} = {}) {
    const calls = [], reads = [];
    let server = {id, name: "Chosen server", bindip: "0.0.0.0", port: 34198, savefile: "selected.zip", use_mimalloc: false};
    const Settings = loadModule("App/views/ServerSettings.jsx", {
        react: React, "react-hook-form": {useForm},
        "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null}, "@fortawesome/free-solid-svg-icons": {},
        "../context/ServerContext": {useServers: () => ({serverID: id, selectedServer: server, refreshServers: async () => {}, refreshServerStatus: async () => {}})},
        "../context/ProfileContext": {useProfiles: () => ({activeProfile: {id: "legacy-world"}, applyProfileState() {}})},
        "../../api/resources/settings": {server: {listForServer: async target => {reads.push(["settings", target]); return {name: "Chosen world"};}, update: async data => calls.push(["settings", data])}},
        "../../api/resources/saves": {listForServer: async target => {reads.push(["saves", target]); return [{name: "selected.zip"}, {name: "new.zip"}];}},
        "../../api/resources/servers": {
            get: async target => { reads.push(["startup", target]); return server; },
            update: async (target, data) => {calls.push(["server", target, data]); server = {...server, ...data, savefile: data.selected_save || server.savefile}; return server;}
        },
        "../../api/resources/profiles": {updateStartup: async (...args) => {calls.push(["legacy", ...args]); server = {...server, bindip: args[1].bind_ip, port: args[1].port, savefile: args[1].selected_save}; return {}; }},
        "../../api/resources/server": {autostart: async () => ({enabled: false}), mapSnapshot: async () => {reads.push(["maps"]); return {settings: {}};}},
        "../components/PageHeader": () => null, "../components/Panel": props => props.content,
        "../components/Input": () => null, "../components/InputPassword": () => null,
        "../components/Label": () => null, "../components/Select": () => null,
        "../components/Error": () => null, "../components/Checkbox": () => null,
        "../components/Button": Button, "../components/Alert": props => props.children,
        "../components/ScopeBadge": () => null,
        "./mapSnapshotSettings.cjs": {mapSnapshotFormValues: () => ({}), mapSnapshotRequestValues: values => values},
        "./ModPortalPreferences": () => null
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => { root = create(React.createElement(Settings, {canManage: true, serverStatus: {known: true, running, stopping: false}})); });
    t.after(() => act(() => root.unmount()));
    return {root, calls, reads};
}

test("startup and multiplayer reads and writes target the selected additional server", async t => {
    const view = await settings(t);
    assert.ok(view.reads.filter(read => read[0] !== "maps").every(read => read[1] === "2"));
    assert.ok(!view.reads.some(read => read[0] === "maps"));
    assert.doesNotMatch(JSON.stringify(view.root.toJSON()), /Process options/);
    await act(async () => view.root.root.findByProps({id: "startup-settings-form"}).props.onSubmit({bind_ip: "127.0.0.2", port: 34200, savefile: "new.zip", use_mimalloc: true}));
    assert.deepEqual(view.calls, [["server", "2", {bind_ip: "127.0.0.2", port: 34200, selected_save: "new.zip", use_mimalloc: true}]]);
    await act(async () => view.root.root.findByProps({id: "server-settings-form"}).props.onSubmit({name: "Second only"}));
    assert.deepEqual(view.calls[1], ["settings", {name: "Second only"}]);
});

test("a running server saves only the next-start allocator preference without restarting it", async t => {
    const view = await settings(t, {running: true});
    const fields = view.root.root.findByProps({id: "startup-settings-form"}).findAllByType("fieldset");
    assert.equal(fields[0].props.disabled, true);
    assert.equal(fields[1].props.disabled, false);
    await act(async () => view.root.root.findByProps({id: "startup-settings-form"}).props.onSubmit({bind_ip: "0.0.0.0", port: 34198, savefile: "selected.zip", use_mimalloc: true}));
    assert.deepEqual(view.calls, [["server", "2", {use_mimalloc: true}]]);
});

test("Server 1 startup retains legacy world persistence without exposing profile switching", async t => {
    const view = await settings(t, {id: "1"});
    await act(async () => view.root.root.findByProps({id: "startup-settings-form"}).props.onSubmit({bind_ip: "0.0.0.0", port: 34200, savefile: "new.zip", use_mimalloc: false}));
    assert.deepEqual(view.calls, [["legacy", "legacy-world", {bind_ip: "0.0.0.0", port: 34200, selected_save: "new.zip"}]]);
});

test("save selection and native downloads refer to the selected server, not the legacy profile", async t => {
    const Saves = loadModule("App/views/Saves/Saves.jsx", {
        react: React, "@fortawesome/react-fontawesome": {FontAwesomeIcon: () => null}, "@fortawesome/free-solid-svg-icons": {},
        "../../../api/resources/saves": {listForServer: async () => [{name: "selected.zip", last_mod: 0, size: 1024}, {name: "other.zip", last_mod: 0, size: 512}]},
        "../../context/ServerContext": {useServers: () => ({serverID: "2", selectedServer: {id: "2", name: "Second", savefile: "selected.zip"}})},
        "../../components/PageHeader": () => null, "../../components/Panel": props => props.content,
        "../../components/IconButton": () => null, "../../components/Button": Button,
        "../../components/Alert": props => props.children, "../../components/ConfirmDialog": () => null,
        "../../components/ScopeBadge": () => null, "../../components/EmptyState": () => null,
        "./components/Checkpoints": () => {throw new Error("Secondary server must not fetch default checkpoints");},
        "./components/NewWorldForm": () => null, "./components/UploadSaveForm": () => null
    }, {window: {flash() {}}}).default;
    let root;
    await act(async () => {root = create(React.createElement(Saves, {canManage: true, serverStatus: {known: true, running: false}}));});
    t.after(() => act(() => root.unmount()));
    const rows = root.root.findAllByType("tr");
    assert.ok(rows[1].findAllByType("span").some(span => span.children.includes("Selected")));
    assert.ok(!rows[2].findAllByType("span").some(span => span.children.includes("Selected")));
    assert.ok(root.root.findAllByType("a").every(link => link.props.href.endsWith("?server=2")));
});
