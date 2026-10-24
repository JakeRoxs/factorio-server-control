import client from "../client";

export default {
    list: async (latest) => {
        const response = await client.get('/api/saves/list', {
            params: {
                latest
            }
        });
        return response.data;
    },
    listForServer: async (serverID, latest) => {
        const response = await client.get(`/api/servers/${serverID}/saves/list`, {
            params: {
                latest
            }
        });
        return response.data;
    },
    delete: async (save) => {
        const response = await client.delete(`/api/saves/rm/${encodeURIComponent(save.name)}`);
        return response.data;
    },
    create: async (name) => {
        const response = await client.post(`/api/saves/create/${encodeURIComponent(name)}`);
        return response.data;
    },
    generation: {
        options: async () => {
            const response = await client.get('/api/saves/generation/options');
            return response.data;
        },
        preview: async settings => {
            try {
                const response = await client.post('/api/saves/generation/preview', settings, {
                    responseType: 'blob'
                });
                return response.data;
            } catch (error) {
                if (error?.response?.data instanceof Blob) {
                    error.response.data = await error.response.data.text();
                }
                throw error;
            }
        },
        create: async settings => {
            const response = await client.post('/api/saves/generation/create', settings);
            return response.data;
        }
    },
    upload: async file => {
        let formData = new FormData();
        formData.append("savefile", file);

        const response = await client.post(`/api/saves/upload`, formData, {
            headers: {
                "Content-Type": "multipart/form-data"
            }
        });
        return response.data;
    },
    mods: async save => {
        const response = await client.post("/api/saves/mods", {
            saveFile: save
        });
        return response.data;
    },
    importMods: async (save, mode = "match") => {
        const response = await client.post("/api/saves/mods/import", {
            saveFile: save,
            mode: mode
        });
        return response.data;
    },
    checkpoints: {
        list: async (serverID) => {
            const response = await client.get(`/api/servers/${encodeURIComponent(serverID)}/checkpoints`);
            return response.data;
        },
        settings: async (serverID, settings) => {
            const response = await client.put(`/api/servers/${encodeURIComponent(serverID)}/checkpoints/settings`, settings);
            return response.data;
        },
        create: async (serverID) => {
            const response = await client.post(`/api/servers/${encodeURIComponent(serverID)}/checkpoints`);
            return response.data;
        },
        restore: async (serverID, checkpoint) => {
            const response = await client.post(`/api/servers/${encodeURIComponent(serverID)}/checkpoints/${encodeURIComponent(checkpoint.id)}/restore`);
            return response.data;
        },
        delete: async (serverID, checkpoint) => {
            const response = await client.delete(`/api/servers/${encodeURIComponent(serverID)}/checkpoints/${encodeURIComponent(checkpoint.id)}`);
            return response.data;
        }
    }
}
