import React, {useEffect, useState} from "react";
import {FontAwesomeIcon} from "@fortawesome/react-fontawesome";
import {faPaperPlane, faTerminal} from "@fortawesome/free-solid-svg-icons";
import client from "../../api/client";
import log from "../../api/resources/log";
import {useServers} from "../context/ServerContext";
import PageHeader from "../components/PageHeader";
import Panel from "../components/Panel";
import Button from "../components/Button";
import Alert from "../components/Alert";
import LogViewport from "../components/LogViewport";
import ScopeBadge from "../components/ScopeBadge";

const Console = ({serverStatus}) => {
    const {serverID} = useServers();
    const [logs, setLogs] = useState([]);
    const [command, setCommand] = useState("");
    const [sending, setSending] = useState(false);
    const [loadError, setLoadError] = useState("");
    const running = Boolean(serverStatus?.running && !serverStatus?.stopping);

    useEffect(() => {
        let active = true;
        const refresh = async () => {
            try {
                const lines = await log.tailForServer(serverID);
                if (active) { setLogs(lines || []); setLoadError(""); }
            } catch {
                if (active) setLoadError("Console output could not be loaded.");
            }
        };
        refresh();
        const timer = window.setInterval(refresh, 3000);
        return () => {
            active = false;
            window.clearInterval(timer);
        };
    }, [serverID]);

    const sendCommand = async event => {
        event.preventDefault();
        const trimmed = command.trim();
        if (!trimmed || !running || sending) return;
        setSending(true);
        try {
            const response = await client.post(`/api/servers/${encodeURIComponent(serverID)}/command`, {command: trimmed});
            if (typeof response.data === "string" && response.data) setLogs(lines => [...lines, response.data]);
            setCommand("");
        } catch {
            // The shared API client reports the error; keep the draft for retry.
        } finally {
            setSending(false);
        }
    };

    return <>
        <PageHeader title="Console"/>
        {!running && <Alert type="warning" className="mb-5">Start Factorio to open its live console.</Alert>}
        {loadError && <Alert type="danger" className="mb-5">{loadError}</Alert>}
        <Panel
            title="Factorio output"
            headerAction={<div className="flex flex-wrap items-center gap-2"><ScopeBadge/><span className="ui-status-badge">{logs.length} lines</span></div>}
            content={<>
                <LogViewport
                    lines={logs}
                    ariaLive
                    label="Live Factorio console output"
                    emptyContent={running ? <p className="text-gray-light">Waiting for Factorio output…</p> : null}
                />
                <form className="mt-3 flex gap-2" onSubmit={sendCommand}>
                    <div className="relative flex-1">
                        <FontAwesomeIcon className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-light" icon={faTerminal}/>
                        <input
                            className="ui-input pl-10 font-mono"
                            value={command}
                            onChange={event => setCommand(event.target.value)}
                            disabled={!running || sending}
                            placeholder="/help"
                            aria-label="Console command"
                        />
                    </div>
                    <Button isSubmit isLoading={sending} isDisabled={!running || sending || !command.trim()}><FontAwesomeIcon icon={faPaperPlane}/> Send</Button>
                </form>
            </>}
        />
    </>;
};

export default Console;
