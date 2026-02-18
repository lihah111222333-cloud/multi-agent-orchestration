// Debug Wails runtime bridge (ESM)
const METHOD_IDS = Object.freeze({
    CALL_API: 1055257995,
    GET_BUILD_INFO: 3168473285,
    GET_GROUP: 4127719990,
    SAVE_CLIPBOARD_IMAGE: 3932748547,
    SELECT_FILES: 937743440,
    SELECT_PROJECT_DIR: 373469749,
});

function appOrThrow() {
    const app = window.go?.main?.App;
    if (!app) throw new Error('debug runtime: window.go.main.App not ready');
    return app;
}

export const Call = {
    ByID: async (methodID, ...args) => {
        const app = appOrThrow();
        switch (methodID) {
            case METHOD_IDS.CALL_API:
                return app.CallAPI(args[0], args[1]);
            case METHOD_IDS.GET_BUILD_INFO:
                return app.GetBuildInfo ? app.GetBuildInfo() : '{}';
            case METHOD_IDS.GET_GROUP:
                return app.GetGroup ? app.GetGroup() : '';
            case METHOD_IDS.SAVE_CLIPBOARD_IMAGE:
                return app.SaveClipboardImage ? app.SaveClipboardImage(args[0]) : '';
            case METHOD_IDS.SELECT_FILES:
                return app.SelectFiles ? app.SelectFiles() : [];
            case METHOD_IDS.SELECT_PROJECT_DIR:
                return app.SelectProjectDir ? app.SelectProjectDir() : '';
            default:
                throw new Error('debug runtime: unknown methodID ' + methodID);
        }
    },
};

export const Events = {
    On: (eventName, callback) => {
        if (!window.runtime?.EventsOn) throw new Error('debug runtime: EventsOn not ready');
        window.runtime.EventsOn(eventName, callback);
        return () => {
            try {
                window.runtime?.EventsOff?.(eventName, callback);
            } catch {
                // ignore
            }
        };
    },
    Off: (eventName) => {
        window.runtime?.EventsOff?.(eventName);
    },
};

export default { Call, Events };
