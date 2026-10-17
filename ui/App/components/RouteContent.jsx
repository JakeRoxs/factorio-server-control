import React, {Component, Suspense} from "react";
import Alert from "./Alert";
import Button from "./Button";

// A failed chunk (for example an old tab after an update) must not take away
// navigation or server controls. Never auto-reload and discard a user's work.
export default class RouteContent extends Component {
    state = {failed: false};

    static getDerivedStateFromError() {
        return {failed: true};
    }

    render() {
        if (this.state.failed) return <Alert type="danger">
            <div className="flex flex-wrap items-center gap-3">
                <span>This page could not be loaded. Reload to try again with the current UI.</span>
                <Button type="secondary" onClick={() => window.location.reload()}>Reload page</Button>
            </div>
        </Alert>;

        return <Suspense fallback={<div className="ui-empty-state" role="status">Loading page…</div>}>
            {this.props.children}
        </Suspense>;
    }
}
