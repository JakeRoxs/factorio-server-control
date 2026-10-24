import React, {useEffect, useState} from "react";
import {Link} from "react-router-dom";
import profilesResource from "../../api/resources/profiles";
import serversResource from "../../api/resources/servers";
import {useServers} from "../context/ServerContext";
import PageHeader from "../components/PageHeader";
import Panel from "../components/Panel";
import Button from "../components/Button";
import Alert from "../components/Alert";
import ConfirmDialog from "../components/ConfirmDialog";
import LegacyProfiles from "./LegacyProfiles";

const Profiles = ({canManage = false, ...props}) => {
    const {serverID, servers, selectedServer, serverStatus, refreshServers} = useServers();
    const [presets, setPresets] = useState([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [sourceID, setSourceID] = useState(serverID);
    const [name, setName] = useState("");
    const [description, setDescription] = useState("");
    const [busy, setBusy] = useState(false);
    const [pending, setPending] = useState(null);
    const load = async () => {
        try {
            const result = await profilesResource.list();
            setPresets((result?.profiles || []).filter(profile => profile.is_global));
            setError("");
        } catch {
            setError("Profile presets could not be loaded.");
        } finally { setLoading(false); }
    };
    useEffect(() => { load(); }, []);
    const source = servers.find(entry => entry.id === sourceID);
    const sourceBusy = !source || (sourceID === serverID
        ? !serverStatus.known || serverStatus.running || serverStatus.stopping
        : source.running || source.stopping);
    const targetBusy = !selectedServer || !serverStatus.known || serverStatus.running || serverStatus.stopping;

    const capture = async event => {
        event.preventDefault();
        if (!canManage || sourceBusy || busy || !name.trim()) return;
        setBusy(true);
        try {
            await serversResource.createProfile(sourceID, name.trim(), description.trim());
            setName(""); setDescription("");
            await load();
            window.flash("Profile preset captured. Saved worlds and credentials were not copied.", "green");
        } catch {
            // The shared client displays the API's actionable error.
        } finally { setBusy(false); }
    };
    const apply = async () => {
        if (!pending || !canManage || targetBusy || busy) return;
        setBusy(true);
        try {
            await serversResource.applyGlobalProfile(serverID, pending.id);
            await refreshServers();
            setPending(null);
            window.flash(`Profile preset applied to ${selectedServer.name}. Its world was preserved.`, "green");
        } finally { setBusy(false); }
    };

    return <>
        <PageHeader title="Profile presets" help="Configure a server once, capture its setup, then reuse it on other servers. Switch servers in navigation for everyday management."/>
        <Panel title="Configure, capture, reuse" className="mb-5" content={<>
            <p className="mb-4">A profile preset contains mods and supported gameplay settings. Applying it preserves the destination server's world, ports, identity and credentials. Later changes to a source server do not automatically change saved presets or other servers.</p>
            <div className="flex flex-wrap gap-3">
                <Link className="ui-button ui-button--secondary" to={`/mods?server=${encodeURIComponent(serverID)}`}>Configure mods</Link>
                <Link className="ui-button ui-button--secondary" to={`/server-settings?server=${encodeURIComponent(serverID)}`}>Configure server settings</Link>
            </div>
        </>}/>
        {canManage && <Panel title="Capture profile preset" className="mb-5" content={<form onSubmit={capture} className="space-y-4">
            <div><label htmlFor="preset-source">Source server</label><select id="preset-source" className="ui-input mt-2" value={sourceID} onChange={event => setSourceID(event.target.value)}>{servers.map(entry => <option key={entry.id} value={entry.id}>{entry.name || `Server ${entry.id}`}</option>)}</select></div>
            <div><label htmlFor="preset-name">Name</label><input id="preset-name" className="ui-input mt-2" value={name} maxLength={100} required onChange={event => setName(event.target.value)}/></div>
            <div><label htmlFor="preset-description">Description</label><input id="preset-description" className="ui-input mt-2" value={description} onChange={event => setDescription(event.target.value)}/></div>
            {sourceBusy && <p className="text-sm text-gray-light">Stop the source server and confirm its status before capturing. Other servers can keep running.</p>}
            <Button isSubmit isLoading={busy} isDisabled={sourceBusy || busy || !name.trim()}>Capture preset</Button>
        </form>}/>}
        <Panel title="Reusable presets" content={<>
            <p className="mb-4">Apply to: <strong>{selectedServer?.name || `Server ${serverID}`}</strong>. Stop this server before applying a preset.</p>
            {error && <Alert type="danger">{error} <Button type="secondary" onClick={load}>Retry</Button></Alert>}
            {loading ? <p>Loading presets...</p> : !error && presets.length === 0 ? <p>No presets captured yet.</p> : presets.map(preset => <article key={preset.id} className="ui-subcard p-4 mb-3">
                <h3 className="font-bold">{preset.name}</h3>
                <p className="my-2 text-sm text-gray-light">{preset.description || "Reusable mods and supported settings"}</p>
                {canManage && <Button type="secondary" isDisabled={targetBusy || busy} onClick={() => setPending(preset)}>Apply to {selectedServer?.name || `Server ${serverID}`}</Button>}
            </article>)}
        </>}/>
        {serverID === "1" && <details className="ui-subcard p-5 mt-5">
            <summary className="cursor-pointer font-bold">Legacy world snapshots and backups</summary>
            <p className="my-4">Older profiles retain their world snapshots and import/export tools. Activating one replaces Server 1's setup and is separate from applying a reusable preset.</p>
            <LegacyProfiles {...props} canManage={canManage}/>
        </details>}
        <ConfirmDialog isOpen={Boolean(pending)} title="Apply profile preset?" content={<>
            Apply <strong>{pending?.name}</strong> to <strong>{selectedServer?.name}</strong>? Mods and supported settings will be replaced. This server's saved world and credentials will be preserved.
        </>} close={() => setPending(null)} onSuccess={apply} loading={busy}/>
    </>;
};

export default Profiles;
