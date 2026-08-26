package com.healthlogin.app.net;

import android.content.Context;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.net.Proxy;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;

/**
 * The channel policy engine. Every 10 minutes (and once at startup) it:
 * <ol>
 *   <li>refreshes the endpoint list (picks up address changes);</li>
 *   <li>health-checks the backend <b>directly</b> — if that works, we run DIRECT
 *       and the tunnel is torn down (direct is the default);</li>
 *   <li>otherwise falls back to the VPN: if the current server still answers we
 *       keep it, else we try each server in the list, giving up on one after 5s
 *       and moving to the next.</li>
 * </ol>
 * All of this is invisible to the WebView/UI.
 */
public final class ChannelManager {
    private static final String TAG = "VlessChannelManager";
    private static final long PERIOD_MINUTES = 10;
    private static final long PROBE_TIMEOUT_SEC = 5;

    private static ChannelManager INSTANCE;

    private final VlessChannel channel = VlessChannel.get();
    private final RemoteConfigRepo repo;
    private final XrayController xray;
    private final String healthUrl;
    private final int socksPort;
    private final OkHttpClient directClient;
    private final OkHttpClient proxyClient;
    private final ScheduledExecutorService exec =
            Executors.newSingleThreadScheduledExecutor(r -> {
                Thread t = new Thread(r, "vless-channel");
                t.setDaemon(true);
                return t;
            });

    private ChannelManager(Context ctx, NetConfig cfg) {
        this.healthUrl = cfg.healthUrl;
        this.socksPort = cfg.socksPort;
        LibXrayBridge lib = LibXrayBridge.tryLoad();
        this.xray = new XrayController(ctx, lib);
        this.repo = new RemoteConfigRepo(ctx, cfg.configUrl);
        this.repo.seedFromAssetsIfEmpty(ctx, "vless-endpoints.enc");

        // Direct probe must never ride the tunnel, even while we are on PROXY.
        this.directClient = new OkHttpClient.Builder()
                .proxy(Proxy.NO_PROXY)
                .callTimeout(PROBE_TIMEOUT_SEC, TimeUnit.SECONDS)
                .build();
        // Proxy probe always goes through the local SOCKS on the fixed port.
        this.proxyClient = new OkHttpClient.Builder()
                .proxy(channel.socksProxy())
                .callTimeout(PROBE_TIMEOUT_SEC, TimeUnit.SECONDS)
                .build();
    }

    /** Install the channel and start the 10-minute evaluation loop. Idempotent. */
    public static synchronized void start(Context ctx, NetConfig cfg) {
        if (INSTANCE != null) return;
        VlessChannel.get().configure(cfg.apiHost, "127.0.0.1", cfg.socksPort);
        INSTANCE = new ChannelManager(ctx, cfg);
        INSTANCE.exec.scheduleWithFixedDelay(
                INSTANCE::safeEvaluate, 0, PERIOD_MINUTES, TimeUnit.MINUTES);
        Log.i(TAG, "channel manager started (default DIRECT)");
    }

    private void safeEvaluate() {
        try { evaluate(); } catch (Throwable t) { Log.e(TAG, "evaluate crashed", t); }
    }

    private void evaluate() {
        repo.refresh(); // learn address changes for this and future cycles

        // 1) Default: try direct.
        if (healthOk(directClient)) {
            if (channel.isProxy()) Log.i(TAG, "direct restored -> DIRECT");
            xray.stop();
            channel.setDirect();
            return;
        }

        if (!xray.available()) { channel.setDirect(); return; }

        // 2) Direct is dead. Keep the current server if it is still up (no blip).
        if (channel.isProxy() && xray.isRunning() && healthOk(proxyClient)) {
            return;
        }

        // 3) Try each server in priority order; 5s each, then next.
        JSONArray configs = repo.configs();
        for (int i = 0; i < configs.length(); i++) {
            JSONObject cfg = configs.optJSONObject(i);
            if (cfg == null) continue;
            String remark = cfg.optString("remarks", "server-" + i);
            if (xray.startWith(cfg, socksPort) && healthOk(proxyClient)) {
                channel.setProxy(remark);
                Log.i(TAG, "channel=PROXY via " + remark);
                return;
            }
            xray.stop();
        }

        // 4) Nothing worked — stay direct and let requests fail as if backend down.
        channel.setDirect();
        Log.w(TAG, "no channel available; staying DIRECT");
    }

    private boolean healthOk(OkHttpClient client) {
        try (Response r = client.newCall(new Request.Builder().url(healthUrl).build()).execute()) {
            return r.isSuccessful();
        } catch (Throwable t) {
            return false;
        }
    }
}
