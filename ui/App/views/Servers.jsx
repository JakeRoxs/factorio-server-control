import React, {useCallback, useEffect, useState} from "react";
import {FontAwesomeIcon} from "@fortawesome/react-fontawesome";
import {
    faPlay, faStop, faTrash, faSave, faPlus, faServer, faEdit,
    faLayerGroup, faDownload, faCheck, faSpinner, faHardDrive, faPuzzlePiece, faCog, faFileLines
} from "@fortawesome/free-solid-svg-icons";
import serversResource from "../../api/resources/servers";
import profilesResource from "../../api/resources/profiles";
import socket from "../../api/socket";
import PageHeader from "../components/PageHeader";
import Panel from "../components/Panel";
import Button from "../components/Button";
import Alert from "../components/Alert";
import ConfirmDialog from "../components/ConfirmDialog";
import NewWorldForm from "./Saves/components/NewWorldForm";

const Servers = ({canManage = false, serverStatus = {known: false}}) => {
    const [servers, setServers] = useState([]);
    const [isLoading, setIsLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [showCreateForm, setShowCreateForm] = useState(false);
    const [editingServer, setEditingServer] = useState(null);
    const [showGlobalProfiles, setShowGlobalProfiles] = useState(false);
    const [worldTargetServerID, setWorldTargetServerID] = useState(null);
    const [serverStatuses, setServerStatuses] = useState({});
    const [serverProfiles, setServerProfiles] = useState({});
    const [globalProfiles, setGlobalProfiles] = useState([]);
    const [isLoadingProfiles] = useState(false);
    const [createGlobalProfile, setCreateGlobalProfile] = useState({name: "", description: ""});
    const [profileSourceID, setProfileSourceID] = useState("1");
    const [showCreateGlobalForm, setShowCreateGlobalForm] = useState(false);
    const [pendingApply, setPendingApply] = useState(null);
    const [createForm, setCreateForm] = useState({
        name: "",
        version: "stable",
        bind_ip: "0.0.0.0",
        port: 0,
        autostart: false
    });

    const loadServers = useCallback(async () => {
        try {
            const data = await serversResource.list();
            setServers(data || []);
            setLoadError("");
        } catch (error) {
            setLoadError("Servers could not be loaded.");
            console.error("Failed to load servers:", error);
        } finally {
            setIsLoading(false);
        }
    }, []);

    const loadProfilesForServer = useCallback(async (serverID) => {
        try {
            const state = await serversResource.profiles(serverID);
            setServerProfiles(prev => ({...prev, [serverID]: state.profiles || []}));
        } catch (error) {
            console.error(`Failed to load profiles for server ${serverID}:`, error);
        }
    }, []);

    const loadGlobalProfiles = useCallback(async () => {
        try {
            const data = await profilesResource.list();
            const global = (data.profiles || []).filter(profile => profile.is_global);
            setGlobalProfiles(global);
        } catch (error) {
            console.error("Failed to load global profiles:", error);
        }
    }, []);

    useEffect(() => {
        loadServers();
        if (canManage) loadGlobalProfiles();
    }, [loadServers, loadGlobalProfiles, canManage]);

    // Subscribe to per-server status rooms
    useEffect(() => {
        const handlers = [];
        servers.forEach(server => {
            const room = `servers:${server.id}:status`;
            const handler = (status) => {
                setServerStatuses(prev => ({...prev, [server.id]: status}));
            };
            handlers.push({room, handler});
            socket.on(room, handler);
            socket.emit("server status:subscribe", {serverID: server.id});
        });
        return () => {
            handlers.forEach(({room, handler}) => socket.off(room, handler));
            servers.forEach(server => {
                socket.emit("server status:unsubscribe", {serverID: server.id});
            });
        };
    }, [servers.map(s => s.id).join(",")]);

    const handleStartServer = async (server) => {
        try {
            await serversResource.start(server.id, server.bindip, server.port, server.savefile || "Load Latest");
            window.flash(`Started server ${server.name || server.id}`, "green");
            loadServers();
        } catch (error) {
            console.error("Failed to start server:", error);
            window.flash(typeof error?.response?.data === "string" ? error.response.data : "The selected server could not be started.", "red");
        }
    };

    const handleStopServer = async (server) => {
        try {
            await serversResource.stop(server.id);
            window.flash(`Stopped server ${server.name || server.id}`, "green");
            loadServers();
        } catch (error) {
            console.error("Failed to stop server:", error);
            window.flash("The selected server could not be stopped.", "red");
        }
    };

    const handleSaveServer = async (server) => {
        try {
            await serversResource.save(server.id);
            window.flash(`Saved server ${server.name || server.id}`, "green");
        } catch (error) {
            console.error("Failed to save server:", error);
            window.flash("The selected game could not be saved. Check that it is running and RCON is ready.", "red");
        }
    };

    const handleDeleteServer = async (server) => {
        if (!window.confirm(`Delete server ${server.name || server.id}? This cannot be undone.`)) return;
        try {
            await serversResource.delete(server.id);
            window.flash(`Deleted server ${server.name || server.id}`, "green");
            loadServers();
        } catch (error) {
            console.error("Failed to delete server:", error);
        }
    };

    const handleCreateFormChange = (e) => {
        const {name, value} = e.target;
        setCreateForm(prev => ({...prev, [name]: value}));
    };

    const handleCreateServer = async (e) => {
        e.preventDefault();
        try {
            const server = await serversResource.create(
                createForm.name,
                createForm.version,
                createForm.bind_ip,
                createForm.port,
                createForm.autostart
            );
            window.flash(`Created server ${server.name || server.id}`, "green");
            setCreateForm({name: "", version: "stable", bind_ip: "0.0.0.0", port: 0, autostart: false});
            setShowCreateForm(false);
            loadServers();
        } catch (error) {
            console.error("Failed to create server:", error);
            window.flash(typeof error?.response?.data === "string" ? error.response.data : "The server could not be created. Check its version target and game port.", "red");
        }
    };

    const handleEditFormChange = (e) => {
        const {name, value, type, checked} = e.target;
        if (type === "checkbox") {
            setEditingServer(prev => ({...prev, [name]: checked}));
        } else {
            setEditingServer(prev => ({...prev, [name]: value}));
        }
    };

    const handleUpdateServer = async (e) => {
        e.preventDefault();
        try {
            await serversResource.update(editingServer.id, {
                name: editingServer.name,
                use_mimalloc: editingServer.use_mimalloc
            });
            window.flash(`Updated server ${editingServer.name || editingServer.id}`, "green");
            setEditingServer(null);
            loadServers();
        } catch (error) {
            console.error("Failed to update server:", error);
            window.flash("Server changes could not be saved. Please try again.", "red");
        }
    };

    const handleCreateGlobalProfile = async (e) => {
        e.preventDefault();
        try {
            await serversResource.createProfile(profileSourceID, createGlobalProfile.name, createGlobalProfile.description);
            window.flash("Reusable preset captured without copying the saved world", "green");
            setCreateGlobalProfile({name: "", description: ""});
            setShowCreateGlobalForm(false);
            loadGlobalProfiles();
        } catch (error) {
            console.error("Failed to create global profile:", error);
            window.flash(typeof error?.response?.data === "string" ? error.response.data : "The preset could not be captured. Check that the source server is stopped and its version and mods are available.", "red");
        }
    };

    const handleAssignProfile = async (serverID, profileID) => {
        try {
            await serversResource.assignProfile(serverID, profileID);
            window.flash("Profile assigned to server", "green");
            loadProfilesForServer(serverID);
            loadServers();
        } catch (error) {
            console.error("Failed to assign profile:", error);
            window.flash("The profile could not be assigned to the selected server.", "red");
        }
    };

    const handleApplyGlobalProfile = async (serverID, profileID) => {
        try {
            await serversResource.applyGlobalProfile(serverID, profileID);
            window.flash("Global profile applied to server", "green");
            loadProfilesForServer(serverID);
            loadServers();
        } catch (error) {
            console.error("Failed to apply global profile:", error);
            window.flash("The profile could not be applied to the selected server.", "red");
        }
    };

    const sourceServer = servers.find(server => server.id === profileSourceID);
    const sourceBusy = sourceServer?.running || sourceServer?.stopping ||
        (serverStatus.id === profileSourceID && (serverStatus.running || serverStatus.stopping));

    if (isLoading) {
        return (
            <div>
                <PageHeader title="Servers"/>
                <p>Loading servers...</p>
            </div>
        );
    }

    if (loadError) {
        return (
            <div>
                <PageHeader title="Servers"/>
                <Alert variant="danger">{loadError}</Alert>
            </div>
        );
    }

    return (
        <div>
            <PageHeader title="Servers" help="Instances own their runtime data and ports, but currently share the installed Factorio executable. Other management pages still address Server 1."/>

            {canManage && (
                <div className="mb-4 flex space-x-3">
                    {!showCreateForm ? (
                        <Button onClick={() => setShowCreateForm(true)}>
                            <FontAwesomeIcon icon={faPlus}/> New Server
                        </Button>
                    ) : (
                        <Panel content={
                            <>
                                <h3>Create New Server</h3>
                                <form onSubmit={handleCreateServer} className="space-y-3">
                                    <div>
                                        <label htmlFor="create-server-name" className="block text-sm font-medium text-gray-700 dark:text-gray-300">Name</label>
                                        <input
                                            id="create-server-name"
                                            type="text"
                                            name="name"
                                            value={createForm.name}
                                            onChange={handleCreateFormChange}
                                            className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                                        />
                                    </div>
                                    <div>
                                        <label htmlFor="create-server-version" className="block text-sm font-medium text-gray-700 dark:text-gray-300">Version target</label>
                                        <input
                                            id="create-server-version"
                                            type="text"
                                            name="version"
                                            value={createForm.version}
                                            onChange={handleCreateFormChange}
                                            className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                                        />
                                        <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">An exact version must match the installed engine. Independent engine installations are not available yet.</p>
                                    </div>
                                    <div className="flex space-x-3">
                                        <Button isSubmit>Create</Button>
                                        <Button type="button" variant="secondary" onClick={() => setShowCreateForm(false)}>Cancel</Button>
                                    </div>
                                </form>
                            </>
                        }/>
                    )}
                    <Button variant="secondary" onClick={() => { setShowGlobalProfiles(!showGlobalProfiles); if (!showGlobalProfiles) loadGlobalProfiles(); }}>
                        <FontAwesomeIcon icon={faLayerGroup}/> Global Profiles
                    </Button>
                </div>
            )}

            {showGlobalProfiles && canManage && (
                <Panel className="mb-4" content={<>
                    <h3>Reusable Preset Library</h3>
                    <p className="mb-3 text-sm text-gray-500 dark:text-gray-400">Presets contain mods and supported settings, not worlds, credentials or network identity. Capture requires a stopped source server with a known installed version.</p>
                    {!showCreateGlobalForm ? (
                        <div>
                            <div className="space-y-2 mb-3">
                                {globalProfiles.length === 0 && <p className="text-gray-500">No global profiles.</p>}
                                {globalProfiles.map(p => (
                                    <div key={p.id} className="flex items-center justify-between p-2 border rounded">
                                        <span>{p.name}</span>
                                    </div>
                                ))}
                            </div>
                            <Button size="sm" onClick={() => setShowCreateGlobalForm(true)}>
                                <FontAwesomeIcon icon={faPlus}/> Capture Preset
                            </Button>
                        </div>
                    ) : (
                        <form onSubmit={handleCreateGlobalProfile} className="space-y-3">
                            <div>
                                <label htmlFor="preset-source-server" className="block text-sm font-medium text-gray-700 dark:text-gray-300">Source server</label>
                                <select id="preset-source-server" value={profileSourceID} onChange={event => setProfileSourceID(event.target.value)} className="mt-1 block w-full rounded-md border-gray-300 shadow-sm">
                                    {servers.map(server => <option key={server.id} value={server.id}>{server.name || `Server ${server.id}`}</option>)}
                                </select>
                                {sourceBusy && <p className="text-sm text-amber-600">Stop the selected source server before capturing its preset. Other servers can stay running.</p>}
                            </div>
                            <div>
                                <label htmlFor="global-profile-name" className="block text-sm font-medium text-gray-700 dark:text-gray-300">Name</label>
                                <input
                                    id="global-profile-name"
                                    type="text"
                                    value={createGlobalProfile.name}
                                    onChange={e => setCreateGlobalProfile(prev => ({...prev, name: e.target.value}))}
                                    className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                                />
                            </div>
                            <div>
                                <label htmlFor="global-profile-desc" className="block text-sm font-medium text-gray-700 dark:text-gray-300">Description</label>
                                <input
                                    id="global-profile-desc"
                                    type="text"
                                    value={createGlobalProfile.description}
                                    onChange={e => setCreateGlobalProfile(prev => ({...prev, description: e.target.value}))}
                                    className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                                />
                            </div>
                            <div className="flex space-x-3">
                                <Button isSubmit isDisabled={!sourceServer || sourceBusy}>Create preset</Button>
                                <Button type="button" variant="secondary" onClick={() => setShowCreateGlobalForm(false)}>Cancel</Button>
                            </div>
                        </form>
                    )}
                </>}/>
            )}

            {worldTargetServerID && (
                <Panel className="mb-4" content={<>
                    <div className="flex items-center justify-between mb-3">
                        <h3>
                            Create world for{" "}
                            {servers.find(s => s.id === worldTargetServerID)?.name || `Server ${worldTargetServerID}`}
                        </h3>
                        <Button size="sm" variant="secondary" onClick={() => setWorldTargetServerID(null)}>
                            Cancel
                        </Button>
                    </div>
                    <NewWorldForm
                        serverID={worldTargetServerID}
                        onSuccess={() => {
                            setWorldTargetServerID(null);
                            loadServers();
                        }}
                    />
                </>}/>
            )}

            <div className="space-y-4">
                {servers.map(entry => {
                    const perServerStatus = serverStatuses[entry.id];
                    const server = perServerStatus
                        ? {...entry, ...perServerStatus, name: entry.name, use_mimalloc: entry.use_mimalloc,
                            base_mod_version: entry.base_mod_version || perServerStatus.base_mod_version,
                            fac_version: entry.fac_version || perServerStatus.fac_version}
                        : (serverStatus.known && serverStatus.id === entry.id
                            ? {...entry, ...serverStatus, name: entry.name, use_mimalloc: entry.use_mimalloc,
                                base_mod_version: entry.base_mod_version || serverStatus.base_mod_version,
                                fac_version: entry.fac_version || serverStatus.fac_version}
                            : entry);
                    const profiles = serverProfiles[server.id] || [];
                    return (
                        <Panel key={server.id} content={<>
                            <div className="flex flex-wrap items-center justify-between gap-3">
                                <div className="flex items-center space-x-3">
                                    <FontAwesomeIcon icon={faServer} className={server.running ? "text-green-500" : "text-gray-400"} size="lg"/>
                                    <div>
                                        <h3 className="text-lg font-semibold">{server.name || `Server ${server.id}`}</h3>
                                        <p className="text-sm text-gray-500 dark:text-gray-400">
                                            Port {server.port} • RCON {server.rcon_port} •{" "}
                                            <span className={server.running ? "text-green-600" : "text-gray-400"}>
                                                {server.stopping ? "Stopping" : server.running ? "Running" : "Stopped"}
                                            </span>
                                        </p>
                                        <p className="text-xs text-gray-400">Engine: {server.base_mod_version || server.fac_version || "Unknown"} (shared installation)</p>
                                        {server.savefile && (
                                            <p className="text-xs text-gray-400">Save: {server.savefile}</p>
                                        )}
                                        {server.profile_id && (
                                            <p className="text-xs text-gray-400">
                                                Profile: {profiles.find(p => p.id === server.profile_id)?.name || server.profile_id}
                                            </p>
                                        )}
                                    </div>
                                </div>

                                {canManage && (
                                    <div className="flex space-x-2">
                                        {!server.running ? (
                                            <Button size="sm" isDisabled={server.stopping} onClick={() => handleStartServer(server)}>
                                                <FontAwesomeIcon icon={faPlay}/> Start
                                            </Button>
                                        ) : (
                                            <>
                                                <Button size="sm" onClick={() => handleSaveServer(server)}>
                                                    <FontAwesomeIcon icon={faSave}/> Save
                                                </Button>
                                                <Button size="sm" variant="danger" isDisabled={server.stopping} onClick={() => handleStopServer(server)}>
                                                    <FontAwesomeIcon icon={faStop}/> Stop
                                                </Button>
                                            </>
                                        )}
                                        {!server.running && (
                                            <Button size="sm" variant="secondary" onClick={() => setWorldTargetServerID(server.id)}>
                                                <FontAwesomeIcon icon={faPlus}/> Create World
                                            </Button>
                                        )}
                                        <Button size="sm" variant="secondary" onClick={() => window.location.href = `/saves?server=${server.id}`}>
                                            <FontAwesomeIcon icon={faHardDrive}/> Saves
                                        </Button>
                                        <Button size="sm" variant="secondary" onClick={() => window.location.href = `/mods?server=${server.id}`}>
                                            <FontAwesomeIcon icon={faPuzzlePiece}/> Mods
                                        </Button>
                                        <Button size="sm" variant="secondary" onClick={() => window.location.href = `/server-settings?server=${server.id}`}>
                                            <FontAwesomeIcon icon={faCog}/> Settings
                                        </Button>
                                        <Button size="sm" variant="secondary" onClick={() => window.location.href = `/logs?server=${server.id}`}>
                                            <FontAwesomeIcon icon={faFileLines}/> Logs
                                        </Button>
                                        {!editingServer || editingServer.id !== server.id ? (
                                            <Button size="sm" variant="secondary" onClick={() => setEditingServer({
                                                ...server,
                                                bind_ip: server.bindip,
                                                port: server.port,
                                                name: server.name || "",
                                                autostart: server.autostart,
                                                use_mimalloc: server.use_mimalloc
                                            })}>
                                                <FontAwesomeIcon icon={faEdit}/> Edit
                                            </Button>
                                        ) : null}
                                        {server.id !== "1" && (
                                            <Button size="sm" variant="danger" isDisabled={server.running || server.stopping} aria-label={`Delete ${server.name || server.id}`} onClick={() => handleDeleteServer(server)}>
                                                <FontAwesomeIcon icon={faTrash}/>
                                            </Button>
                                        )}
                                    </div>
                                )}
                            </div>

                            {editingServer && editingServer.id === server.id && (
                                <form onSubmit={handleUpdateServer} className="mt-3 space-y-3 p-3 border rounded bg-gray-50 dark:bg-gray-800">
                                    <div>
                                        <label htmlFor="edit-server-name" className="block text-sm font-medium text-gray-700 dark:text-gray-300">Name</label>
                                        <input
                                            id="edit-server-name"
                                            type="text"
                                            name="name"
                                            value={editingServer.name}
                                            onChange={handleEditFormChange}
                                            className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                                        />
                                    </div>
                                    <div>
                                        <label className="flex items-center space-x-2 text-sm text-gray-700 dark:text-gray-300">
                                            <input
                                                type="checkbox"
                                                name="use_mimalloc"
                                                checked={editingServer.use_mimalloc || false}
                                                onChange={handleEditFormChange}
                                                className="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
                                            />
                                            <span>Use mimalloc memory allocator (Linux only)</span>
                                        </label>
                                        <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">Saved for the next server start. Changing this does not restart or modify a running game.</p>
                                    </div>
                                    <div className="flex space-x-3">
                                        <Button isSubmit>Save Changes</Button>
                                        <Button type="button" variant="secondary" onClick={() => setEditingServer(null)}>Cancel</Button>
                                    </div>
                                </form>
                            )}

                            {canManage && (
                                <div className="mt-3">
                                    <div className="flex items-center space-x-2 mb-2">
                                        <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Profiles</h4>
                                        <Button size="sm" variant="link" onClick={() => loadProfilesForServer(server.id)}>
                                            <FontAwesomeIcon icon={faDownload}/> Load
                                        </Button>
                                    </div>
                                    {isLoadingProfiles && <FontAwesomeIcon icon={faSpinner} spin className="text-sm"/>}
                                    <div className="space-y-1">
                                        {profiles.length === 0 && <p className="text-xs text-gray-400">No profiles loaded.</p>}
                                        {profiles.map(p => (
                                            <div key={p.id} className="flex items-center justify-between text-sm p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700">
                                                <span>
                                                    {p.name}
                                                    {p.is_global && <span className="ml-1 text-xs text-blue-500">(global)</span>}
                                                    {p.id === server.profile_id && <span className="ml-1 text-xs text-green-500">(active)</span>}
                                                </span>
                                                <div className="flex space-x-1">
                                                    {!p.is_global && (
                                                        <Button size="xs" variant="link" onClick={() => handleAssignProfile(server.id, p.id)}>
                                                            <FontAwesomeIcon icon={faCheck}/> Assign
                                                        </Button>
                                                    )}
                                                    {p.is_global && (
                                                        <Button size="xs" variant="link" onClick={() => setPendingApply({serverID: server.id, serverName: server.name || `Server ${server.id}`, profileID: p.id, profileName: p.name})}>
                                                            <FontAwesomeIcon icon={faDownload}/> Apply
                                                        </Button>
                                                    )}
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            )}
                        </>}/>
                    );
                })}
            </div>

            {pendingApply && (
                <ConfirmDialog
                    isOpen={Boolean(pendingApply)}
                    title="Apply profile preset"
                    message={<>
                        <p>Apply preset <strong>{pendingApply.profileName}</strong> to <strong>{pendingApply.serverName}</strong>?</p>
                        <p className="text-sm text-gray-500 mt-2">The server's saved world will be preserved. Only mods and settings from the preset will be applied.</p>
                    </>}
                    confirmLabel="Apply preset"
                    onConfirm={async () => {
                        await handleApplyGlobalProfile(pendingApply.serverID, pendingApply.profileID);
                        setPendingApply(null);
                    }}
                    onCancel={() => setPendingApply(null)}
                />
            )}
        </div>
    );
};

export default Servers;
