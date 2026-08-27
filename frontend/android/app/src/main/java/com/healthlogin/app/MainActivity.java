package com.healthlogin.app;

import android.os.Bundle;

import com.getcapacitor.BridgeActivity;
import com.healthlogin.app.net.ChannelManager;
import com.healthlogin.app.net.NetConfig;
import com.healthlogin.app.net.VlessChannel;
import com.healthlogin.app.plugins.NativeWebSocketPlugin;
import com.healthlogin.app.plugins.UpdatePlugin;
import com.healthlogin.app.plugins.VpnDebugPlugin;

import java.net.ProxySelector;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(UpdatePlugin.class);
        registerPlugin(NativeWebSocketPlugin.class);
        registerPlugin(VpnDebugPlugin.class);

        // Install the process-wide proxy selector BEFORE the WebView/Capacitor
        // issue any request, so Capacitor's HttpURLConnection-based HTTP plugin
        // (axios) transparently honours the fallback channel. In DIRECT mode it
        // returns NO_PROXY; it only tunnels the backend host while on PROXY.
        NetConfig net = NetConfig.production();
        VlessChannel.get().configure(net.apiHost, "127.0.0.1", net.socksPort);
        ProxySelector.setDefault(VlessChannel.get().proxySelector());
        ChannelManager.start(getApplicationContext(), net);

        super.onCreate(savedInstanceState);
    }
}
