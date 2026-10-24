import React, {useCallback, useEffect, useState} from "react";
import {FontAwesomeIcon} from "@fortawesome/react-fontawesome";
import {faPlay, faStop, faSave, faServer} from "@fortawesome/free-solid-svg-icons";
import serversResource from "../../api/resources/servers";
import PageHeader from "../components/PageHeader";
import Panel from "../components/Panel";
import Button from "../components/Button";
import Alert from "../components/Alert";
import ConfirmDialog from "../components/ConfirmDialog";

const Servers = ({canManage = false, serverStatus = {known: false}, refreshServers}) => {
    const [servers, setServers] = useState([]);
    const [isLoading, setIsLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [showCreate, setShowCreate] = useState(false);
    const [editing, setEditing] = useState(null);
    const [deleting, setDeleting] = useState(null);
    const [busy, setBusy] = useState(false);
    const [form, setForm] = useState({name: "", version: "stable", bind_ip: "0.0.0.0", port: 0});

    const loadServers = useCallback(async (refreshContext = false) => {
        try {
            setServers(await serversResource.list() || []);
            setLoadError("");
            if (refreshContext === true) refreshServers?.().catch(() => undefined);
        } catch { setLoadError("Servers could not be loaded."); }
        finally { setIsLoading(false); }
    }, [refreshServers]);
    useEffect(() => {
        loadServers();
        const timer = window.setInterval(loadServers, 5000);
        return () => window.clearInterval(timer);
    }, [loadServers]);

    const operate = async (server, operation) => {
        if (!canManage || busy) return;
        setBusy(true);
        try {
            if (operation === "start") await serversResource.start(server.id, server.bindip, server.port, server.savefile || "Load Latest");
            else await serversResource[operation](server.id);
            await loadServers(true);
            window.flash(operation === "start" ? "Server started." : operation === "stop" ? "Save and stop requested." : "Game saved.", "green");
        } catch {
            window.flash(`The selected server could not ${operation === "save" ? "be saved" : operation}.`, "red");
        } finally { setBusy(false); }
    };
    const create = async event => {
        event.preventDefault();
        if (!canManage || busy) return;
        setBusy(true);
        try {
            await serversResource.create(form.name.trim(), form.version, form.bind_ip, Number(form.port), false);
            await loadServers(true);
            setShowCreate(false);
            setForm({name: "", version: "stable", bind_ip: "0.0.0.0", port: 0});
            window.flash("Server created. Open it to configure its setup and create a world.", "green");
        } catch {
            window.flash("The server could not be created. Check its version and port.", "red");
        } finally { setBusy(false); }
    };
    const rename = async event => {
        event.preventDefault();
        if (!canManage || busy) return;
        setBusy(true);
        try {
            await serversResource.update(editing.id, {name: editing.name.trim()});
            setEditing(null);
            await loadServers(true);
        } catch { window.flash("Server changes could not be saved.", "red"); }
        finally { setBusy(false); }
    };

    return <>
        <PageHeader title="Servers" help="Open a server to manage its own world, mods, settings, console and logs. Switching servers never changes their assigned setups." actions={canManage && <Button onClick={() => setShowCreate(value => !value)}>New server</Button>}/>
        {loadError && <Alert type="danger" className="mb-5">{loadError} <Button type="secondary" onClick={loadServers}>Retry</Button></Alert>}
        {showCreate && canManage && <Panel title="Create server" className="mb-5" content={<form className="space-y-4" onSubmit={create}>
            {[["name", "Name"], ["version", "Version target"], ["bind_ip", "Bind address"], ["port", "Game port (0 selects an available port)"]].map(([key, label]) => <div key={key}>
                <label htmlFor={`create-${key}`}>{label}</label><input id={`create-${key}`} className="ui-input mt-2" name={key} type={key === "port" ? "number" : "text"} min={key === "port" ? 0 : undefined} max={key === "port" ? 65535 : undefined} value={form[key]} required onChange={event => setForm(previous => ({...previous, [key]: event.target.value}))}/>
            </div>)}
            <p className="text-sm text-gray-light">Instances share the installed engine. An exact version must match it. Publish each additional UDP port in your container configuration.</p>
            <div className="flex gap-3"><Button isSubmit isLoading={busy}>Create</Button><Button type="secondary" onClick={() => setShowCreate(false)}>Cancel</Button></div>
        </form>}/>}
        {isLoading && <p>Loading servers...</p>}
        <div className="space-y-5">{servers.map(entry => {
            const server = serverStatus.known && serverStatus.id === entry.id
                ? {...entry, ...serverStatus, name: entry.name, base_mod_version: entry.base_mod_version || serverStatus.base_mod_version}
                : entry;
            const stopped = !server.running && !server.stopping;
            return <Panel key={server.id} content={<>
                <div className="flex flex-wrap items-center justify-between gap-4">
                    <div><h3 className="text-lg font-bold"><FontAwesomeIcon icon={faServer}/> {server.name || `Server ${server.id}`}</h3>
                        <p className="text-sm text-gray-light">Port {server.port} | RCON {server.rcon_port} | <span>{server.stopping ? "Stopping" : server.running ? "Running" : "Stopped"}</span></p>
                        <p className="text-xs text-gray-light">Engine: {server.base_mod_version || server.fac_version || "Unknown"} (shared installation)</p>
                        <p className="text-xs text-gray-light">Save: {server.savefile || "No save selected"}</p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                        <a className="ui-button ui-button--success" href={`/?server=${encodeURIComponent(server.id)}`}>Open server</a>
                        <a className="ui-button ui-button--secondary" href={`/profiles?server=${encodeURIComponent(server.id)}`}>Profile presets</a>
                        {canManage && <>
                            {server.running ? <><Button size="sm" isDisabled={busy || server.stopping} onClick={() => operate(server, "save")}><FontAwesomeIcon icon={faSave}/> Save</Button><Button size="sm" type="danger" isDisabled={busy || server.stopping} onClick={() => operate(server, "stop")}><FontAwesomeIcon icon={faStop}/> Stop</Button></>
                                : <Button size="sm" isDisabled={busy || server.stopping} onClick={() => operate(server, "start")}><FontAwesomeIcon icon={faPlay}/> Start</Button>}
                            <Button size="sm" type="secondary" isDisabled={busy} onClick={() => setEditing({...server, name: server.name || ""})}>Edit</Button>
                            {server.id !== "1" && <Button size="sm" type="danger" isDisabled={busy || !stopped} onClick={() => setDeleting(server)}>Delete</Button>}
                        </>}
                    </div>
                </div>
                {editing?.id === server.id && <form className="mt-4 space-y-3" onSubmit={rename}>
                    <label htmlFor="edit-server-name">Name</label><input id="edit-server-name" className="ui-input" name="name" value={editing.name} required onChange={event => setEditing(previous => ({...previous, name: event.target.value}))}/>
                    <div className="flex gap-3"><Button isSubmit isLoading={busy}>Save changes</Button><Button type="secondary" onClick={() => setEditing(null)}>Cancel</Button></div>
                </form>}
            </>}/>;
        })}</div>
        <ConfirmDialog isOpen={Boolean(deleting)} title="Delete server?" content={`Remove ${deleting?.name || "this server"} from the catalog? Its stored data will be retained.`} close={() => setDeleting(null)} onSuccess={async () => { await serversResource.delete(deleting.id); await loadServers(true); }}/>
    </>;
};

export default Servers;
