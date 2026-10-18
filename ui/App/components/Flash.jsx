import React, {useCallback, useEffect, useRef, useState} from "react";
import {FontAwesomeIcon} from "@fortawesome/react-fontawesome";
import {faCheckCircle, faCircleExclamation, faCircleInfo, faXmark} from "@fortawesome/free-solid-svg-icons";
import Bus from "../../notifications";

const TOAST_DURATION = 5000;

export const Flash = () => {
    const [toast, setToast] = useState(null);
    const [paused, setPaused] = useState(false);
    const timerRef = useRef(null);
    const remainingRef = useRef(TOAST_DURATION);
    const startRef = useRef(0);

    const clearTimer = useCallback(() => {
        if (timerRef.current) {
            window.clearTimeout(timerRef.current);
            timerRef.current = null;
        }
    }, []);

    const startTimer = useCallback(() => {
        clearTimer();
        startRef.current = Date.now();
        const el = document.getElementById("flash-timer-bar");
        if (el) {
            el.style.transition = `width ${remainingRef.current}ms linear`;
            el.style.width = "0%";
        }
        timerRef.current = window.setTimeout(() => setToast(null), remainingRef.current);
    }, [clearTimer]);

    const flashListener = useCallback(({message, color}) => {
        clearTimer();
        remainingRef.current = TOAST_DURATION;
        setToast({message, color});
        setPaused(false);
        requestAnimationFrame(() => {
            const el = document.getElementById("flash-timer-bar");
            if (el) {
                el.style.transition = "none";
                el.style.width = "100%";
                requestAnimationFrame(() => startTimer());
            }
        });
    }, [clearTimer, startTimer]);

    useEffect(() => {
        Bus.addListener("flash", flashListener);
        return () => {
            Bus.removeListener("flash", flashListener);
            clearTimer();
        };
    }, [flashListener, clearTimer]);

    const handleMouseEnter = useCallback(() => {
        if (!toast) return;
        const elapsed = Date.now() - startRef.current;
        remainingRef.current = Math.max(0, remainingRef.current - elapsed);
        clearTimer();
        setPaused(true);
        const el = document.getElementById("flash-timer-bar");
        if (el) {
            const computed = getComputedStyle(el);
            const width = parseFloat(computed.width);
            const container = el.parentElement;
            const containerWidth = container ? container.clientWidth : 0;
            el.style.transition = "none";
            el.style.width = `${(width / containerWidth) * 100}%`;
        }
    }, [toast, clearTimer]);

    const handleMouseLeave = useCallback(() => {
        if (!toast || !paused) return;
        setPaused(false);
        startTimer();
    }, [toast, paused, startTimer]);

    if (!toast) return null;
    const icon = toast.color === "green" ? faCheckCircle : toast.color === "red" ? faCircleExclamation : faCircleInfo;

    return <div className="fixed z-50 right-4 bottom-4 md:right-7 md:bottom-7" role="status" aria-live="polite">
        <div className={`ui-toast ui-toast--${toast.color} relative overflow-hidden`}
            onMouseEnter={handleMouseEnter}
            onMouseLeave={handleMouseLeave}>
            <div className="flex items-start gap-3">
                <FontAwesomeIcon className={toast.color === "green" ? "text-green mt-1" : toast.color === "red" ? "text-red mt-1" : "text-blue mt-1"} icon={icon}/>
                <p className="flex-1 text-sm">{toast.message}</p>
                <button className="text-gray-light hover:text-white" onClick={() => setToast(null)} aria-label="Dismiss notification">
                    <FontAwesomeIcon icon={faXmark}/>
                </button>
            </div>
            <div id="flash-timer-bar"
                className={`absolute bottom-0 left-0 h-1 ${toast.color === "green" ? "bg-green" : toast.color === "red" ? "bg-red" : "bg-blue"}`}
                style={{width: "100%"}}/>
        </div>
    </div>;
};