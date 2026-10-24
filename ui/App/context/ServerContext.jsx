import React, {createContext, useCallback, useContext, useEffect, useRef, useState} from "react";
import {useLocation, useNavigate} from "react-router-dom";
import serversResource from "../../api/resources/servers";
import client from "../../api/client";

const ServerContext = createContext(null);

export const ServerProvider = ({children}) => {
    const location = useLocation();
    const navigate = useNavigate();
    const serverID = new URLSearchParams(location.search).get("server") || "1";
    const [servers, setServers] = useState([]);
    const [error, setError] = useState("");
    const [isLoading, setIsLoading] = useState(true);
    const [status, setStatus] = useState({known: false});
    const currentID = useRef(serverID);
    const statusGeneration = useRef(0);
    currentID.current = serverID;
    const selectedServer = servers.find(entry => entry.id === serverID) || null;
    const serverAvailable = Boolean(selectedServer);

    const refreshServers = useCallback(async () => {
        try {
            const result = await serversResource.list();
            if (!Array.isArray(result)) throw new Error("Invalid server list");
            setServers(result);
            setError("");
            return result;
        } catch (requestError) {
            setError("Servers could not be loaded.");
            throw requestError;
        } finally {
            setIsLoading(false);
        }
    }, []);

    const refreshServerStatus = useCallback(async () => {
        const generation = ++statusGeneration.current;
        try {
            const response = await client.get(`/api/servers/${encodeURIComponent(serverID)}/status`);
            const result = response.data;
            if (typeof result?.running !== "boolean" || typeof result?.stopping !== "boolean") throw new Error("Invalid server status");
            if (currentID.current === serverID && generation === statusGeneration.current) setStatus({...result, id: serverID, known: true});
            return result;
        } catch (requestError) {
            if (currentID.current === serverID && generation === statusGeneration.current) setStatus({id: serverID, known: false});
            throw requestError;
        }
    }, [serverID]);

    useEffect(() => { refreshServers().catch(() => undefined); }, [refreshServers]);
    useEffect(() => {
        let active = true;
        setStatus({id: serverID, known: false});
        if (!serverAvailable) return undefined;
        const refresh = () => {
            if (!active) return;
            refreshServerStatus().catch(() => undefined);
        };
        refresh();
        const timer = window.setInterval(refresh, 5000);
        return () => {
            active = false;
            statusGeneration.current += 1;
            window.clearInterval(timer);
        };
    }, [serverID, serverAvailable, refreshServerStatus]);

    const selectServer = id => {
        if (!servers.some(entry => entry.id === id)) return;
        const search = new URLSearchParams(location.search);
        search.set("server", id);
        navigate({pathname: location.pathname, search: search.toString()});
    };
    const serverStatus = status.id === serverID ? status : {id: serverID, known: false};
    return <ServerContext.Provider value={{serverID, servers, selectedServer, serverStatus, isLoading, error, selectServer, refreshServers, refreshServerStatus}}>
        {children}
    </ServerContext.Provider>;
};

export const useServers = () => {
    const context = useContext(ServerContext);
    if (!context) throw new Error("useServers must be used inside ServerProvider.");
    return context;
};
