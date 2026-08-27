package com.healthlogin.app.plugins;

import com.getcapacitor.JSArray;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import com.healthlogin.app.net.ChannelManager;
import com.healthlogin.app.net.DebugLog;

/**
 * Exposes the VLESS channel's diagnostics to the WebView debug console: the log
 * ring buffer, the live channel state, and a manual re-evaluate trigger. Purely a
 * read/trigger surface — it changes no behaviour, and the frontend only mounts
 * the console when VITE_DEBUG is on.
 */
@CapacitorPlugin(name = "VpnDebug")
public class VpnDebugPlugin extends Plugin {

    @PluginMethod
    public void getLogs(PluginCall call) {
        JSObject ret = new JSObject();
        JSArray lines = new JSArray();
        for (String line : DebugLog.snapshot()) {
            lines.put(line);
        }
        ret.put("lines", lines);
        call.resolve(ret);
    }

    @PluginMethod
    public void getState(PluginCall call) {
        try {
            call.resolve(JSObject.fromJSONObject(ChannelManager.debugState()));
        } catch (Exception e) {
            call.reject("failed to read channel state: " + e.getMessage());
        }
    }

    @PluginMethod
    public void setVerbose(PluginCall call) {
        DebugLog.setVerbose(call.getBoolean("enabled", true));
        call.resolve();
    }

    @PluginMethod
    public void clear(PluginCall call) {
        DebugLog.clear();
        call.resolve();
    }

    @PluginMethod
    public void reevaluate(PluginCall call) {
        ChannelManager.triggerReevaluate();
        call.resolve();
    }
}
