import React, {useEffect, useRef, useState} from "react";
import {Link, NavLink, Outlet, useLocation} from "react-router-dom";
import {FontAwesomeIcon} from "@fortawesome/react-fontawesome";
import {
    faBars, faCloudArrowDown, faFileLines, faFloppyDisk,
    faGamepad, faGaugeHigh, faLayerGroup, faMugHot, faMusic, faPuzzlePiece,
    faRightFromBracket, faServer, faSliders, faTerminal, faUsers, faXmark
} from "@fortawesome/free-solid-svg-icons";
import Button from "./Button";
import ProfileContextBar from "./ProfileContextBar";
import {useServers} from "../context/ServerContext";
import BrandMark from "./BrandMark";
import {focusableElements, lockBodyScroll, trapFocus} from "./overlay";
import RouteContent from "./RouteContent";

const profileLinks = [
    ["Overview", "/", faGaugeHigh],
    ["Saves & Checkpoints", "/saves", faFloppyDisk],
    ["Mods", "/mods", faPuzzlePiece],
    ["Server settings", "/server-settings", faSliders],
    ["Game settings", "/game-settings", faGamepad],
    ["Console", "/console", faTerminal],
    ["Logs", "/logs", faFileLines],
    ["Version & mode", "/releases", faCloudArrowDown]
];

const managerLinks = [
    ["Servers", "/servers", faServer],
    ["Profile presets", "/profiles", faLayerGroup],
    ["Users & access", "/user-management", faUsers]
];

const externalReferenceLinks = [
    ["Factory radio", "https://suno.com/playlist/ccf52e9d-ab58-4950-aea1-b3bb7f2aa585", faMusic],
    ["Support on Ko-fi", "https://ko-fi.com/tricade", faMugHot]
];

const mobileLinks = [
    ["Overview", "/", faGaugeHigh],
    ["Saves", "/saves", faFloppyDisk],
    ["Mods", "/mods", faPuzzlePiece]
];

const isManagerPath = pathname => ["/servers", "/profiles", "/user-management"].some(path => pathname === path || pathname.startsWith(path + "/"));

const NavigationLink = ({item, onNavigate}) => {
    const [label, to, icon] = item;
    const {serverID} = useServers();
    return <NavLink
        end={to === "/"}
        to={`${to}?server=${encodeURIComponent(serverID)}`}
        onClick={onNavigate}
        className={({isActive}) => `ui-nav-link${isActive ? " is-active" : ""}`}
    >
        <FontAwesomeIcon className="ui-nav-link__icon" icon={icon}/>
        <span>{label}</span>
    </NavLink>;
};

const ExternalLink = ({item}) => {
    const [label, href, icon] = item;
    return <a className="ui-nav-link" href={href} target="_blank" rel="noreferrer">
        <FontAwesomeIcon className="ui-nav-link__icon" icon={icon}/>
        <span>{label}</span>
        <span className="ui-nav-link__external" aria-hidden="true">↗</span>
    </a>;
};

const Layout = ({handleLogout, serverStatus, refreshServerStatus, currentUser, socketState, canManage = false}) => {
    const [isNavOpen, setIsNavOpen] = useState(false);
    const [isCompactNavigation, setIsCompactNavigation] = useState(() => window.matchMedia("(max-width: 1023px)").matches);
    const sidebarRef = useRef(null);
    const location = useLocation();
    const {servers, serverID, selectedServer, selectServer, isLoading, error, refreshServers} = useServers();
    const isPrimaryMobileRoute = mobileLinks.some(([, to]) => to === location.pathname);
    const visibleProfileLinks = canManage ? profileLinks : profileLinks.filter(([, path]) => path !== "/console");
    const visibleManagerLinks = canManage
        ? managerLinks
        : managerLinks.map(item => item[1] === "/user-management" ? ["Account & access", item[1], item[2]] : item);
    const outletKey = isManagerPath(location.pathname)
        ? `manager:${location.pathname}`
        : `server:${serverID}`;
    const revision = !["", "unknown", "local"].includes(__FSM_UI_REVISION__) ? ` · ${__FSM_UI_REVISION__.slice(0, 8)}` : "";

    useEffect(() => {
        const media = window.matchMedia("(max-width: 1023px)");
        const update = event => setIsCompactNavigation(event.matches);
        if (media.addEventListener) media.addEventListener("change", update);
        else media.addListener(update);
        return () => {
            if (media.removeEventListener) media.removeEventListener("change", update);
            else media.removeListener(update);
        };
    }, []);

    useEffect(() => {
        const hidden = isCompactNavigation && !isNavOpen;
        sidebarRef.current?.toggleAttribute("inert", hidden);
    }, [isCompactNavigation, isNavOpen]);

    useEffect(() => {
        setIsNavOpen(false);
        window.scrollTo({top: 0, left: 0, behavior: "auto"});
    }, [location.pathname]);

    useEffect(() => {
        if (!isNavOpen || !isCompactNavigation) return undefined;
        const previouslyFocused = document.activeElement;
        // The preceding effect has removed inert. Focus now so a delayed frame
        // cannot steal focus from a profile-picker interaction after opening.
        focusableElements(sidebarRef.current)[0]?.focus();
        const closeOnEscape = event => {
            if (event.key === "Escape") {
                event.preventDefault();
                setIsNavOpen(false);
            }
            trapFocus(event, sidebarRef.current);
        };
        const unlockBodyScroll = lockBodyScroll();
        document.addEventListener("keydown", closeOnEscape);
        return () => {
            unlockBodyScroll();
            document.removeEventListener("keydown", closeOnEscape);
            if (previouslyFocused instanceof HTMLElement && previouslyFocused.isConnected) previouslyFocused.focus();
        };
    }, [isCompactNavigation, isNavOpen]);

    const sidebarContent = <>
        <div className="ui-sidebar__brand">
            <BrandMark/>
            <div className="ui-sidebar__brand-copy">
                <strong>Factorio Server Control</strong>
            </div>
            <button className="ui-sidebar__close" onClick={() => setIsNavOpen(false)} aria-label="Close navigation">
                <FontAwesomeIcon icon={faXmark}/>
            </button>
        </div>

        <div className="ui-profile-menu-wrap p-4">
            <label className="ui-context-label" htmlFor="selected-server">Selected server</label>
            <select id="selected-server" className="ui-input mt-2" value={selectedServer ? serverID : ""} disabled={isLoading || Boolean(error)} onChange={event => selectServer(event.target.value)}>
                {!selectedServer && <option value="">{isLoading ? "Loading servers..." : "Server unavailable"}</option>}
                {servers.map(entry => <option key={entry.id} value={entry.id}>{entry.name || `Server ${entry.id}`}</option>)}
            </select>
            <p className="mt-2 text-xs text-gray-light">Switch servers without changing their profiles or stopping games.</p>
            {error && <button type="button" className="ui-button ui-button--secondary mt-2" onClick={() => refreshServers().catch(() => undefined)}>Retry servers</button>}
        </div>

        <div className="ui-sidebar__scroll">
            <nav aria-label="Primary navigation">
                <div className="ui-nav-section">
                    <p className="ui-nav-section-title">Server</p>
                    {visibleProfileLinks.map(item => <NavigationLink key={item[1]} item={item} onNavigate={() => setIsNavOpen(false)}/>) }
                </div>
                <div className="ui-nav-section">
                    <p className="ui-nav-section-title">Manager</p>
                    {visibleManagerLinks.map(item => <NavigationLink key={item[1]} item={item} onNavigate={() => setIsNavOpen(false)}/>) }
                </div>
                <div className="ui-nav-section">
                    <p className="ui-nav-section-title">Reference</p>
                    {externalReferenceLinks.map(item => <ExternalLink key={item[1]} item={item}/>) }
                </div>
            </nav>
        </div>

        <div className="ui-sidebar__footer">
            <div className="ui-account-row">
                <div className="ui-account-avatar">{(currentUser?.username || "U").slice(0, 1).toUpperCase()}</div>
                <div className="ui-account-copy">
                    <strong>{currentUser?.username || "User"}</strong>
                    <span>{currentUser?.role || "operator"}</span>
                </div>
                <Button type="ghost" size="sm" className="ui-signout-button" onClick={handleLogout} title="Sign out">
                    <FontAwesomeIcon icon={faRightFromBracket}/><span>Sign out</span>
                </Button>
            </div>
            <div className="ui-manager-meta">
                <span
                    className={`ui-connection-dot ui-connection-dot--${socketState}`}
                    title={socketState === "connected" ? "Control connection online" : socketState === "connecting" ? "Control connection is reconnecting" : "Live updates offline"}
                    aria-label={socketState === "connected" ? "Control connection online" : socketState === "connecting" ? "Control connection is reconnecting" : "Live updates offline"}
                />
                <span className="ui-manager-product" title="Factorio Server Control">Factorio Server Control</span>
                <span className="ui-manager-version" title={`Factorio Server Control UI revision ${__FSM_UI_REVISION__}`}>v{__FSM_UI_VERSION__}{revision}</span>
            </div>
        </div>
    </>;

    return <div className="ui-app-shell">
        <header className="ui-mobile-appbar">
            <button className="ui-mobile-menu" onClick={event => {
                event.currentTarget.focus({preventScroll: true});
                setIsNavOpen(true);
            }} aria-label="Open navigation">
                <FontAwesomeIcon icon={faBars}/>
            </button>
            <BrandMark/>
            <div className="ui-mobile-brand-copy">
                <strong>Factorio Server Control</strong>
                <span>{selectedServer?.name || "Server unavailable"}</span>
            </div>
        </header>

        {isNavOpen && <button className="ui-sidebar-scrim" onClick={() => setIsNavOpen(false)} aria-label="Close navigation overlay"/>}
        <aside
            ref={sidebarRef}
            className={`ui-sidebar${isNavOpen ? " is-open" : ""}`}
            aria-label="Application navigation"
            aria-hidden={isCompactNavigation && !isNavOpen ? "true" : undefined}
        >
            {sidebarContent}
        </aside>

        <main className="ui-main">
            <ProfileContextBar serverStatus={serverStatus} refreshServerStatus={refreshServerStatus} canManage={canManage}/>
            <div className="ui-page">
                <RouteContent key={`${outletKey}:${location.pathname}`}>
                    <Outlet/>
                </RouteContent>
            </div>
        </main>

        <nav className="ui-mobile-tabbar" aria-label="Mobile navigation">
            {mobileLinks.map(([label, to, icon]) => <NavLink
                key={to}
                end={to === "/"}
                to={`${to}?server=${encodeURIComponent(serverID)}`}
                className={({isActive}) => `ui-mobile-tab${isActive ? " is-active" : ""}`}
            >
                <FontAwesomeIcon icon={icon}/><span>{label}</span>
            </NavLink>)}
            <button className={`ui-mobile-tab${isPrimaryMobileRoute ? "" : " is-active"}`} type="button" onClick={() => setIsNavOpen(true)}>
                <FontAwesomeIcon icon={faBars}/><span>More</span>
            </button>
        </nav>
    </div>;
};

export default Layout;
