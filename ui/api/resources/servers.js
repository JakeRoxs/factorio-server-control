import client from "../client";

export default {
    list: async () => {
        const response = await client.get('/api/servers');
        return response.data;
    },
    previewNext: async () => {
        const response = await client.get('/api/servers/next');
        return response.data;
    },
    create: async (name, version, bindIP, port, autostart) => {
        const response = await client.post('/api/servers', {
            name,
            version,
            bind_ip: bindIP,
            port,
            autostart
        });
        return response.data;
    },
    get: async (id) => {
        const response = await client.get(`/api/servers/${id}`);
        return response.data;
    },
    update: async (id, data) => {
        const response = await client.put(`/api/servers/${id}`, data);
        return response.data;
    },
    delete: async (id) => {
        const response = await client.delete(`/api/servers/${id}`);
        return response.data;
    },
    save: async (id) => {
        const response = await client.post(`/api/servers/${id}/save`);
        return response.data;
    },
    start: async (id, bindip, port, savefile) => {
        const response = await client.post(`/api/servers/${encodeURIComponent(id)}/start`, {bindip, port, savefile});
        return response.data;
    },
    stop: async (id) => {
        const response = await client.post(`/api/servers/${encodeURIComponent(id)}/stop`);
        return response.data;
    },
    profiles: async (id) => {
        const response = await client.get(`/api/servers/${id}/profiles`);
        return response.data;
    },
    createProfile: async (id, name, description) => {
        const response = await client.post(`/api/servers/${id}/profiles`, {
            name,
            description
        });
        return response.data;
    },
    assignProfile: async (id, profileID) => {
        const response = await client.post(`/api/servers/${id}/profiles/${profileID}/assign`);
        return response.data;
    },
    createGlobalProfile: async (name, description) => {
        const response = await client.post('/api/profiles/global', {
            name,
            description
        });
        return response.data;
    },
    applyGlobalProfile: async (serverID, profileID) => {
        const response = await client.post(`/api/servers/${serverID}/profiles/${profileID}/apply`);
        return response.data;
    },
    createWorld: async (serverID, request) => {
        const response = await client.post(`/api/servers/${serverID}/saves/generation/create`, request);
        return response.data;
    }
};
