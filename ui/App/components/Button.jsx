import React from "react";
import {faSpinner} from "@fortawesome/free-solid-svg-icons";
import {FontAwesomeIcon} from "@fortawesome/react-fontawesome";

const Button = ({children, type = "default", onClick, isSubmit, className = "", size, isLoading, isDisabled = false, title, form, "aria-label": ariaLabel}) => {
    const variant = ["success", "danger", "secondary", "ghost"].includes(type) ? type : "default";

    return (
        <button
            onClick={event => {
                // Safari may not focus clicked buttons. Dialogs need their
                // trigger focused before opening to restore it on dismissal.
                event.currentTarget.focus({preventScroll: true});
                onClick?.(event);
            }}
            disabled={isDisabled || isLoading}
            className={`${className} ui-button ui-button--${variant}${size === "sm" ? " ui-button--sm" : ""}`}
            type={isSubmit ? "submit" : "button"}
            form={form}
            aria-busy={isLoading || undefined}
            aria-label={ariaLabel}
            title={title}
        >
            {isLoading && <FontAwesomeIcon icon={faSpinner} spin={true}/>} {children}
        </button>
    );
}

export default Button;
