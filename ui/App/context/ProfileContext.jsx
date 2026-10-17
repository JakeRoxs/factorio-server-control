import React, {createContext, useCallback, useContext, useEffect, useMemo, useRef, useState} from "react";
import profilesResource from "../../api/resources/profiles";

const ProfileContext = createContext(null);

const errorText = error => {
    const value = error?.response?.data?.error || error?.response?.data || error?.message;
    return typeof value === "string" && value.trim() ? value.trim() : "Profiles could not be loaded.";
};

const validateState = result => {
    const profiles = result?.profiles;
    if (!Array.isArray(profiles) || typeof result.active_profile_id !== "string" ||
        !profiles.every(profile => typeof profile?.id === "string" && profile.id.length > 0) ||
        new Set(profiles.map(profile => profile.id)).size !== profiles.length ||
        !profiles.some(profile => profile.id === result.active_profile_id)) {
        throw new Error("The profile API returned an invalid response.");
    }
    return result;
};

export const ProfileProvider = ({children}) => {
    const [state, setState] = useState(null);
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState("");
    const hasLoaded = useRef(false);
    const generation = useRef(0);
    const mounted = useRef(true);

    const applyProfileState = useCallback(result => {
        const validated = validateState(result);
        // Explicit mutations supersede any older in-flight list response.
        generation.current += 1;
        if (mounted.current) {
            setState(validated);
            setError("");
            hasLoaded.current = true;
            setIsLoading(false);
        }
        return validated;
    }, []);

    const refreshProfiles = useCallback(async () => {
        const requestGeneration = ++generation.current;
        const isCurrent = () => mounted.current && requestGeneration === generation.current;
        if (!hasLoaded.current) setIsLoading(true);
        try {
            const result = validateState(await profilesResource.list());
            if (isCurrent()) {
                setState(result);
                setError("");
            }
            return result;
        } catch (requestError) {
            if (isCurrent()) setError(errorText(requestError));
            throw requestError;
        } finally {
            if (isCurrent()) {
                hasLoaded.current = true;
                setIsLoading(false);
            }
        }
    }, []);

    useEffect(() => {
        mounted.current = true;
        refreshProfiles().catch(() => undefined);
        return () => {
            mounted.current = false;
            generation.current += 1;
        };
    }, [refreshProfiles]);

    const activeProfile = useMemo(
        () => state?.profiles?.find(profile => profile.id === state.active_profile_id) || null,
        [state]
    );

    const value = useMemo(() => ({
        state,
        activeProfile,
        isLoading,
        error,
        applyProfileState,
        refreshProfiles
    }), [state, activeProfile, isLoading, error, applyProfileState, refreshProfiles]);

    return <ProfileContext.Provider value={value}>{children}</ProfileContext.Provider>;
};

export const useProfiles = () => {
    const context = useContext(ProfileContext);
    if (!context) throw new Error("useProfiles must be used inside ProfileProvider.");
    return context;
};
