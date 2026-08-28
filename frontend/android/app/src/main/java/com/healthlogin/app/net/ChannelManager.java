package com.healthlogin.app.net;

import android.content.Context;

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
 * Every decision and every probe request/response is written to {@link DebugLog}
 * so the debug console can show exactly why the proxy path did or did not connect.
 */
public final class ChannelManager {
    private static final String TAG = "channel";
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

        DebugLog.add(TAG, "manager init: apiHost=" + cfg.apiHost + " health=" + healthUrl
                + " config=" + cfg.configUrl + " socks=127.0.0.1:" + socksPort
                + " libXray=" + (xray.available() ? "loaded" : "MISSING"));
    }

    /** Install the channel and start the 10-minute evaluation loop. Idempotent. */
    public static synchronized void start(Context ctx, NetConfig cfg) {
        if (INSTANCE != null) return;
        VlessChannel.get().configure(cfg.apiHost, "127.0.0.1", cfg.socksPort);
        INSTANCE = new ChannelManager(ctx, cfg);
        INSTANCE.exec.scheduleWithFixedDelay(
                INSTANCE::safeEvaluate, 0, PERIOD_MINUTES, TimeUnit.MINUTES);
        DebugLog.add(TAG, "manager started (default DIRECT)");
    }

    private void safeEvaluate() {
        try { evaluate(); } catch (Throwable t) {
            DebugLog.add(TAG, "evaluate crashed: " + t.getClass().getSimpleName() + ": " + t.getMessage());
        }
    }

    private void evaluate() {
        DebugLog.add(TAG, "---- evaluate (current=" + (channel.isProxy()
                ? "PROXY:" + channel.activeRemark() : "DIRECT") + ") ----");

        boolean changed = repo.refresh();
        DebugLog.add(TAG, "config " + (changed ? "updated" : "unchanged") + ": " + repo.summary());

        // 1) Default: try direct.
        if (healthOk(directClient, "direct")) {
            if (channel.isProxy()) DebugLog.add(TAG, "direct restored -> DIRECT");
            xray.stop();
            channel.setDirect();
            return;
        }
        DebugLog.add(TAG, "direct is down -> trying proxy fallback");

        if (!xray.available()) {
            DebugLog.add(TAG, "libXray missing -> staying DIRECT (drop in app/libs/*.aar)");
            channel.setDirect();
            return;
        }

        // 2) Direct is dead. Keep the current server if it still answers.
        if (channel.isProxy() && xray.isRunning() && healthOk(proxyClient, "proxy(current)")) {
            DebugLog.add(TAG, "kept current server " + channel.activeRemark());
            return;
        }

        // 3) Try each server in priority order; 5s each, then next.
        JSONArray configs = repo.configs();
        DebugLog.add(TAG, "iterating " + configs.length() + " server(s)");
        for (int i = 0; i < configs.length(); i++) {
            JSONObject cfg = configs.optJSONObject(i);
            if (cfg == null) continue;
            String remark = cfg.optString("remarks", "server-" + i);
            boolean started = xray.startWith(cfg, socksPort);
            if (started) {
                boolean ok = healthOk(proxyClient, "proxy(" + remark + ")");
                xray.drainXrayLog();
                if (ok) {
                    channel.setProxy(remark);
                    DebugLog.add(TAG, "SELECTED PROXY via " + remark);
                    return;
                }
            }
            xray.stop();
        }

        // 4) Nothing worked — stay direct and let requests fail as if backend down.
        channel.setDirect();
        DebugLog.add(TAG, "no channel available; staying DIRECT");
    }

    /** Run the health request and record the full request/response line. */
    private boolean healthOk(OkHttpClient client, String via) {
        long t0 = System.currentTimeMillis();
        Request req = new Request.Builder().url(healthUrl).header("Cache-Control", "no-cache").build();
        try (Response r = client.newCall(req).execute()) {
            long ms = System.currentTimeMillis() - t0;
            DebugLog.add("probe", via + " GET " + healthUrl + " -> HTTP " + r.code()
                    + " " + r.message() + " (" + ms + "ms)");
            return r.isSuccessful();
        } catch (Throwable t) {
            long ms = System.currentTimeMillis() - t0;
            DebugLog.add("probe", via + " GET " + healthUrl + " -> ERROR "
                    + t.getClass().getSimpleName() + ": " + t.getMessage() + " (" + ms + "ms)");
            return false;
        }
    }

    // --- debug surface for VpnDebugPlugin ---

    /** A JSON snapshot of the live channel state and what is stored. */
    public static JSONObject debugState() {
        JSONObject o = new JSONObject();
        try {
            VlessChannel c = VlessChannel.get();
            o.put("mode", c.isProxy() ? "PROXY" : "DIRECT");
            o.put("activeRemark", c.activeRemark() == null ? "" : c.activeRemark());
            o.put("socksPort", c.socksPort());
            if (INSTANCE != null) {
                o.put("healthUrl", INSTANCE.healthUrl);
                o.put("libXray", INSTANCE.xray.available() ? "loaded" : "missing");
                o.put("stored", INSTANCE.repo.summary());
            }
        } catch (Throwable ignored) { }
        return o;
    }

    /** Force an out-of-cycle re-evaluation (used by the debug console button). */
    public static void triggerReevaluate() {
        if (INSTANCE != null) {
            DebugLog.add(TAG, "manual re-evaluate requested");
            INSTANCE.exec.execute(INSTANCE::safeEvaluate);
        }
    }
}
