package com.healthlogin.app.net;

import android.content.Context;
import android.content.res.AssetManager;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;

/**
 * Owns the single libXray instance: normalises a full Xray config (rewrites the
 * inbound to our fixed local SOCKS), writes it to a file, and starts/stops the
 * core. One instance per process — every start() stops the previous one.
 */
final class XrayController {
    private static final String TAG = "XrayController";
    private static final String[] GEO_FILES = { "geoip.dat", "geosite.dat" };

    private final Context ctx;
    private final LibXrayBridge lib;
    private final String datDir;
    private volatile boolean running = false;

    XrayController(Context ctx, LibXrayBridge lib) {
        this.ctx = ctx.getApplicationContext();
        this.lib = lib;
        File d = new File(this.ctx.getFilesDir(), "xray_assets");
        if (!d.exists()) d.mkdirs();
        this.datDir = d.getAbsolutePath();
        copyGeoAssets();
    }

    boolean available() { return lib != null; }
    boolean isRunning() { return running; }

    /** Start the core with one of the full configs from the endpoint list. */
    boolean startWith(JSONObject cfg, int socksPort) {
        if (lib == null) return false;
        stop();
        try {
            String json = normalize(cfg, socksPort);
            File f = new File(ctx.getCacheDir(), "xray_live.json");
            write(f, json);
            running = lib.run(datDir, f.getAbsolutePath(), json);
            if (!running) Log.w(TAG, "libXray failed to start config");
            return running;
        } catch (Throwable t) {
            Log.e(TAG, "startWith failed", t);
            running = false;
            return false;
        }
    }

    void stop() {
        if (lib != null && running) {
            try { lib.stop(); } catch (Throwable t) { Log.w(TAG, "stop failed", t); }
        }
        running = false;
    }

    /**
     * Replace whatever inbound the config shipped with our fixed 127.0.0.1 SOCKS
     * inbound, and drop client-only fields the core does not need. The outbounds
     * (vless/reality/xhttp/vision, blackhole, freedom) are passed through as-is.
     */
    private String normalize(JSONObject cfg, int socksPort) throws Exception {
        JSONObject c = new JSONObject(cfg.toString()); // deep copy

        JSONObject socks = new JSONObject()
                .put("protocol", "socks")
                .put("tag", "socks")
                .put("listen", "127.0.0.1")
                .put("port", socksPort)
                .put("settings", new JSONObject().put("udp", true))
                .put("sniffing", new JSONObject()
                        .put("enabled", true)
                        .put("routeOnly", false)
                        .put("destOverride", new JSONArray().put("tls").put("quic").put("http")));

        c.put("inbounds", new JSONArray().put(socks));
        c.remove("stats");    // stats API needs a matching inbound; we do not use it
        c.remove("policy");
        c.remove("remarks");
        c.remove("id");
        return c.toString();
    }

    private void copyGeoAssets() {
        AssetManager am = ctx.getAssets();
        for (String name : GEO_FILES) {
            File out = new File(datDir, name);
            if (out.exists()) continue;
            try (InputStream in = am.open(name)) {
                copy(in, out);
                Log.i(TAG, "copied geo asset " + name);
            } catch (Throwable ignored) {
                // Not bundled — fine; our configs use no geo routing rules.
            }
        }
    }

    private static void write(File f, String content) throws Exception {
        try (OutputStream os = new FileOutputStream(f)) {
            os.write(content.getBytes("UTF-8"));
        }
    }

    private static void copy(InputStream in, File out) throws Exception {
        try (OutputStream os = new FileOutputStream(out)) {
            byte[] buf = new byte[8192];
            int n;
            while ((n = in.read(buf)) != -1) os.write(buf, 0, n);
        }
    }
}
