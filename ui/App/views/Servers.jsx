import React, {useCallback, useEffect, useState} from "react";
import {FontAwesomeIcon} from "@fortawesome/react-fontawesome";
import {
    faPlay, faStop, faTrash, faSave, faPlus, faServer, faEdit,
    faLayerGroup, faDownload, faCheck, faSpinner
} from "@fortawesome/free-solid-svg-icons";
import serversResource from "../../api/resources/servers";
import serverResource from "../../api/resources/server";
import PageHeader from "../components/PageHeader";
import Panel from "../components/Panel";
import Button from "../components/Button";
import Alert from "../components/Alert";
import socket from "../../api/socket";

const Servers = ({canManage = false}) => {
    const [servers, setServers] = useState([]);
    const [isLoading, setIsLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [showCreateForm, setShowCreateForm] = useState(false);
    const [editingServer, setEditingServer] = useState(null);
    const [showGlobalProfiles, setShowGlobalProfiles] = useState(false);
    const [serverProfiles, setServerProfiles] = useState({});
    const [globalProfiles, setGlobalProfiles] = useState([]);
    const [isLoadingProfiles] = useState(false);
    const [createGlobalProfile, setCreateGlobalProfile] = useState({name: "", description: ""});
    const [showCreateGlobalForm, setShowCreateGlobalForm] = useState(false);
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
            const data = await serversResource.list();
            const global = (data || []).filter(s => s.is_global);
            setGlobalProfiles(global);
        } catch (error) {
            console.error("Failed to load global profiles:", error);
        }
    }, []);

    useEffect(() => {
        loadServers();
        if (canManage) loadGlobalProfiles();
    }, [loadServers, loadGlobalProfiles, canManage]);

    useEffect(() => {
        if (!canManage) return undefined;
        socket.emit("servers subscribe");
        const handler = () => {
            loadServers();
            servers.forEach(s => loadProfilesForServer(s.id));
        };
        socket.on("servers", handler);
        return () => {
            socket.off("servers", handler);
            socket.emit("servers unsubscribe");
        };
    }, [canManage, loadServers, servers, loadProfilesForServer]);

    const handleStartServer = async (server) => {
        try {
            await serverResource.start(server.bindip, server.port, server.savefile || "Load Latest");
            window.flash(`Started server ${server.name || server.id}`, "green");
            loadServers();
        } catch (error) {
            console.error("Failed to start server:", error);
        }
    };

    const handleStopServer = async (server) => {
        try {
            await serverResource.stop();
            window.flash(`Stopped server ${server.name || server.id}`, "green");
            loadServers();
        } catch (error) {
            console.error("Failed to stop server:", error);
        }
    };

    const handleSaveServer = async (server) => {
        try {
            await serversResource.save(server.id);
            window.flash(`Saved server ${server.name || server.id}`, "green");
        } catch (error) {
            console.error("Failed to save server:", error);
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
                bind_ip: editingServer.bind_ip,
                port: editingServer.port,
                autostart: editingServer.autostart,
                use_mimalloc: editingServer.use_mimalloc
            });
            window.flash(`Updated server ${editingServer.name || editingServer.id}`, "green");
            setEditingServer(null);
            loadServers();
        } catch (error) {
            console.error("Failed to update server:", error);
        }
    };

    const handleCreateGlobalProfile = async (e) => {
        e.preventDefault();
        try {
            await serversResource.createGlobalProfile(createGlobalProfile.name, createGlobalProfile.description);
            window.flash("Created global profile", "green");
            setCreateGlobalProfile({name: "", description: ""});
            setShowCreateGlobalForm(false);
            loadGlobalProfiles();
        } catch (error) {
            console.error("Failed to create global profile:", error);
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
        }
    };

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
            <PageHeader title="Servers"/>

            {canManage && (
                <div className="mb-4 flex space-x-3">
                    {!showCreateForm ? (
                        <Button onClick={() => setShowCreateForm(true)}>
                            <FontAwesomeIcon icon={faPlus}/> New Server
                        </Button>
                    ) : (
                        <Panel>
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
                                    <label htmlFor="create-server-version" className="block text-sm font-medium text-gray-700 dark:text-gray-300">Version</label>
                                    <input
                                        id="create-server-version"
                                        type="text"
                                        name="version"
                                        value={createForm.version}
                                        onChange={handleCreateFormChange}
                                        className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm"
                                    />
                                </div>
                                <div className="flex space-x-3">
                                    <Button type="submit">Create</Button>
                                    <Button type="button" variant="secondary" onClick={() => setShowCreateForm(false)}>Cancel</Button>
                                </div>
                            </form>
                        </Panel>
                    )}
                    <Button variant="secondary" onClick={() => { setShowGlobalProfiles(!showGlobalProfiles); if (!showGlobalProfiles) loadGlobalProfiles(); }}>
                        <FontAwesomeIcon icon={faLayerGroup}/> Global Profiles
                    </Button>
                </div>
            )}

            {showGlobalProfiles && canManage && (
                <Panel className="mb-4">
                    <h3>Global Profile Templates</h3>
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
                                <FontAwesomeIcon icon={faPlus}/> New Global Profile
                            </Button>
                        </div>
                    ) : (
                        <form onSubmit={handleCreateGlobalProfile} className="space-y-3">
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
                                <Button type="submit">Create</Button>
                                <Button type="button" variant="secondary" onClick={() => setShowCreateGlobalForm(false)}>Cancel</Button>
                            </div>
                        </form>
                    )}
                </Panel>
            )}

            <div className="space-y-4">
                {servers.map(server => {
                    const profiles = serverProfiles[server.id] || [];
                    return (
                        <Panel key={server.id}>
                            <div className="flex items-center justify-between">
                                <div className="flex items-center space-x-3">
                                    <FontAwesomeIcon icon={faServer} className={server.running ? "text-green-500" : "text-gray-400"} size="lg"/>
                                    <div>
                                        <h3 className="text-lg font-semibold">{server.name || `Server ${server.id}`}</h3>
                                        <p className="text-sm text-gray-500 dark:text-gray-400">
                                            Port {server.port} • RCON {server.rcon_port} •{" "}
                                            <span className={server.running ? "text-green-600" : "text-gray-400"}>
                                                {server.running ? "Running" : "Stopped"}
                                            </span>
                                        </p>
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
                                            <Button size="sm" onClick={() => handleStartServer(server)}>
                                                <FontAwesomeIcon icon={faPlay}/> Start
                                            </Button>
                                        ) : (
                                            <>
                                                <Button size="sm" onClick={() => handleSaveServer(server)}>
                                                    <FontAwesomeIcon icon={faSave}/> Save
                                                </Button>
                                                <Button size="sm" variant="danger" onClick={() => handleStopServer(server)}>
                                                    <FontAwesomeIcon icon={faStop}/> Stop
                                                </Button>
                                            </>
                                        )}
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
                                        <Button size="sm" variant="danger" onClick={() => handleDeleteServer(server)}>
                                            <FontAwesomeIcon icon={faTrash}/>
                                        </Button>
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
                                    </div>
                                    <div className="flex space-x-3">
                                        <Button type="submit">Save Changes</Button>
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
                                                        <Button size="xs" variant="link" onClick={() => handleApplyGlobalProfile(server.id, p.id)}>
                                                            <FontAwesomeIcon icon={faDownload}/> Apply
                                                        </Button>
                                                    )}
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            )}
                        </Panel>
                    );
                })}
            </div>
        </div>
    );
};

export default Servers;
