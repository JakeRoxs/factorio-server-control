import React, {lazy, useCallback, useEffect, useState} from "react";
import {BrowserRouter, Navigate, Outlet, Route, Routes} from "react-router-dom";
import user from "../api/resources/user";
import socket from "../api/socket";
import Layout from "./components/Layout";
import Login from "./views/Login";
import {Flash} from "./components/Flash";
import {ProfileProvider} from "./context/ProfileContext";
import {ServerProvider, useServers} from "./context/ServerContext";
import BrandMark from "./components/BrandMark";
import {authenticationRequiredEvent} from "../api/client";

// Keep authentication and the navigation shell available while a page loads.
const Controls = lazy(() => import(/* webpackChunkName: "overview" */ "./views/Controls"));
const Logs = lazy(() => import(/* webpackChunkName: "logs" */ "./views/Logs"));
const Saves = lazy(() => import(/* webpackChunkName: "saves" */ "./views/Saves/Saves"));
const Mods = lazy(() => import(/* webpackChunkName: "mods" */ "./views/Mods/Mods"));
const UserManagement = lazy(() => import(/* webpackChunkName: "users" */ "./views/UserManagement/UserManagment"));
const ServerSettings = lazy(() => import(/* webpackChunkName: "server-settings" */ "./views/ServerSettings"));
const GameSettings = lazy(() => import(/* webpackChunkName: "game-settings" */ "./views/GameSettings"));
const Console = lazy(() => import(/* webpackChunkName: "console" */ "./views/Console"));
const Releases = lazy(() => import(/* webpackChunkName: "releases" */ "./views/Releases"));
const Profiles = lazy(() => import(/* webpackChunkName: "profiles" */ "./views/Profiles"));
const Servers = lazy(() => import(/* webpackChunkName: "servers" */ "./views/Servers"));

const AppLoader = () => <div className="ui-login-shell">
    <div className="text-center">
        <BrandMark className="mx-auto mb-4" isLoading/>
        <p className="font-bold">Connecting to Factorio Server Control</p>
    </div>
</div>;

const ProtectedRoute = ({authState}) => {
    if (authState === "checking") return <AppLoader/>;
    if (authState !== "authenticated") return <Navigate to="/login" replace state={{from: window.location.pathname}}/>;
    return <Outlet/>;
};

const ServerPage = ({component: Component, ...props}) => {
    const {serverID, selectedServer, serverStatus, refreshServerStatus, refreshServers, isLoading} = useServers();
    if (!selectedServer && Component !== Servers) return <div role="status" className="ui-empty-state">
        {isLoading ? "Loading selected server..." : "This server is unavailable. Select an existing server from navigation."}
    </div>;
    return <Component key={serverID} {...props} serverStatus={serverStatus} refreshServerStatus={refreshServerStatus} refreshServers={refreshServers}/>;
};

const Workspace = props => {
    const {selectedServer, serverStatus, refreshServerStatus} = useServers();
    return <ProfileProvider server={selectedServer}><Layout {...props} serverStatus={serverStatus} refreshServerStatus={refreshServerStatus}/></ProfileProvider>;
};

const ServerRedirect = ({to}) => {
    const {serverID} = useServers();
    return <Navigate to={`${to}?server=${encodeURIComponent(serverID)}`} replace/>;
};

const App = () => {
    const [authState, setAuthState] = useState("checking");
    const [currentUser, setCurrentUser] = useState(null);
    const [socketState, setSocketState] = useState("disconnected");
    const canManage = currentUser?.role === "admin";

    const handleAuthenticated = useCallback(async authenticatedUser => {
        setCurrentUser(authenticatedUser);
        setAuthState("authenticated");
    }, []);

    useEffect(() => {
        const requireAuthentication = () => {
            socket.disconnect();
            setCurrentUser(null);
            setAuthState("anonymous");
        };
        window.addEventListener(authenticationRequiredEvent, requireAuthentication);
        return () => window.removeEventListener(authenticationRequiredEvent, requireAuthentication);
    }, []);

    useEffect(() => {
        let active = true;
        user.status()
            .then(status => active && handleAuthenticated(status))
            .catch(() => active && setAuthState("anonymous"));
        return () => { active = false; };
    }, [handleAuthenticated]);

    useEffect(() => {
        if (authState !== "authenticated") {
            socket.disconnect();
            return undefined;
        }

        const receiveConnectionState = state => setSocketState(state);

        socket.on("connection_state", receiveConnectionState);
        socket.connect();

        return () => {
            socket.off("connection_state", receiveConnectionState);
            socket.disconnect();
        };
    }, [authState]);

    const handleLogout = useCallback(async () => {
        try {
            await user.logout();
        } finally {
            socket.disconnect();
            setCurrentUser(null);
            setAuthState("anonymous");
        }
    }, []);

    return <BrowserRouter>
        <Routes>
            <Route path="login" element={
                authState === "authenticated"
                    ? <Navigate to="/" replace/>
                    : <Login handleLogin={handleAuthenticated} isChecking={authState === "checking"}/>
            }/>
            <Route element={<ProtectedRoute authState={authState}/> }>
                <Route element={<ServerProvider><Workspace handleLogout={handleLogout} currentUser={currentUser} socketState={socketState} canManage={canManage}/></ServerProvider>}>
                    <Route index element={<ServerPage component={Controls} canManage={canManage}/>}/>
                    <Route path="saves" element={<ServerPage component={Saves} canManage={canManage}/>}/>
                    <Route path="mods" element={<ServerPage component={Mods} canManage={canManage}/>}/>
                    <Route path="server-settings" element={<ServerPage component={ServerSettings} canManage={canManage}/>}/>
                    <Route path="game-settings" element={<ServerPage component={GameSettings}/>}/>
                    <Route path="console" element={canManage ? <ServerPage component={Console} socketState={socketState}/> : <ServerRedirect to="/"/>}/>
                    <Route path="logs" element={<ServerPage component={Logs}/>}/>
                    <Route path="releases" element={<ServerPage component={Releases} canManage={canManage}/>}/>
                    <Route path="profiles" element={<ServerPage component={Profiles} canManage={canManage}/>}/>
                    <Route path="servers" element={<ServerPage component={Servers} canManage={canManage}/>}/>
                    <Route path="user-management" element={<UserManagement currentUser={currentUser} canManage={canManage}/>}/>
                </Route>
            </Route>
            <Route path="*" element={<Navigate to="/" replace/>}/>
        </Routes>
        <Flash/>
    </BrowserRouter>;
};

export default App;
