import React from "react";

const ButtonLink = ({children, href, type = "default", target, className = "", size, download}) => {
    const variant = ["success", "danger", "secondary", "ghost"].includes(type) ? type : "default";
    const scopedHref = href?.startsWith("/api/")
        ? `${href}${href.includes("?") ? "&" : "?"}server=${encodeURIComponent(new URLSearchParams(window.location.search).get("server") || "1")}`
        : href;

    return (
        <a
            href={scopedHref}
            target={target || "_self"}
            rel={target === "_blank" ? "noreferrer" : undefined}
            download={download}
            className={`${className} ui-button ui-button--${variant}${size === "sm" ? " ui-button--sm" : ""}`}>
            {children}
        </a>
    );
}

export default ButtonLink;
