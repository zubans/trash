package com.healthlogin.app.net;

import android.content.Context;
import android.content.res.AssetManager;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.io.RandomAccessFile;
import java.net.InetSocketAddress;
import java.net.Socket;

/**
 * Owns the single libXray instance: normalises a full Xray config (rewrites the
 * inbound to our fixed local SOCKS, forces a resolver, turns on logging), writes
 * it to a file, starts the core and waits for the SOCKS port to accept
 * connections before it is used. One instance per process — every start() stops
 * the previous one.
 */
final class XrayController {
    private static final String TAG = "xray";
    private static final String[] GEO_FILES = { "geoip.dat", "geosite.dat" };
    // How long to wait for the SOCKS listener to accept a connection after start.
    private static final int PORT_READY_TIMEOUT_MS = 4000;

    private final Context ctx;
    private final LibXrayBridge lib;
    private final String datDir;
    private final File errorLog;
    private volatile boolean running = false;
    private long errorLogOffset = 0;

    XrayController(Context ctx, LibXrayBridge lib) {
        this.ctx = ctx.getApplicationContext();
        this.lib = lib;
        File d = new File(this.ctx.getFilesDir(), "xray_assets");
        if (!d.exists()) d.mkdirs();
        this.datDir = d.getAbsolutePath();
        this.errorLog = new File(this.ctx.getCacheDir(), "xray_error.log");
        copyGeoAssets();
    }

    boolean available() { return lib != null; }
    boolean isRunning() { return running; }

    /** Start the core with one of the full configs and wait until SOCKS is up. */
    boolean startWith(JSONObject cfg, int socksPort) {
        if (lib == null) {
            DebugLog.add(TAG, "libXray not loaded — cannot start proxy");
            return false;
        }
        stop();
        // A fresh error log per attempt keeps the tail relevant to this server.
        errorLog.delete();
        errorLogOffset = 0;
        try {
            String json = normalize(cfg, socksPort);
            DebugLog.add(TAG, "starting " + summarizeOutbound(cfg) + " (core " + lib.version() + ")");
            File f = new File(ctx.getCacheDir(), "xray_live.json");
            write(f, json);
            boolean started = lib.run(datDir, f.getAbsolutePath(), json);
            drainXrayLog();
            if (!started) {
                DebugLog.add(TAG, "libXray.run() returned failure");
                running = false;
                return false;
            }
            running = true;
            boolean ready = waitForPort("127.0.0.1", socksPort, PORT_READY_TIMEOUT_MS);
            drainXrayLog();
            if (!ready) {
                DebugLog.add(TAG, "SOCKS 127.0.0.1:" + socksPort + " did not become ready in "
                        + PORT_READY_TIMEOUT_MS + "ms");
                return false;
            }
            DebugLog.add(TAG, "SOCKS 127.0.0.1:" + socksPort + " ready");
            return true;
        } catch (Throwable t) {
            DebugLog.add(TAG, "startWith failed: " + t.getClass().getSimpleName() + ": " + t.getMessage());
            running = false;
            return false;
        }
    }

    void stop() {
        if (lib != null && running) {
            try { lib.stop(); } catch (Throwable t) {
                DebugLog.add(TAG, "stop failed: " + t.getMessage());
            }
        }
        running = false;
    }

    /** Push any new lines libXray appended to its error log into the debug buffer. */
    void drainXrayLog() {
        if (!errorLog.exists()) return;
        try (RandomAccessFile raf = new RandomAccessFile(errorLog, "r")) {
            long len = raf.length();
            if (len < errorLogOffset) errorLogOffset = 0; // file was rotated/truncated
            raf.seek(errorLogOffset);
            String line;
            while ((line = raf.readLine()) != null) {
                String s = new String(line.getBytes("ISO-8859-1"), "UTF-8").trim();
                if (!s.isEmpty()) DebugLog.add("xray-core", s);
            }
            errorLogOffset = raf.getFilePointer();
        } catch (Throwable t) {
            DebugLog.add(TAG, "could not read xray log: " + t.getMessage());
        }
    }

    /**
     * Replace whatever inbound the config shipped with our fixed 127.0.0.1 SOCKS
     * inbound, force a public resolver so the VLESS server hostname resolves even
     * when the device resolver is poisoned (the direct path being blocked is why
     * we are here), and log at debug level to a file we can tail. The outbounds
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

        // Resolve names (the VLESS server host, and sniffed destinations) through
        // public resolvers instead of the device one. "localhost" keeps the
        // server's own address resolvable via the system if it is an IP.
        c.put("dns", new JSONObject().put("servers",
                new JSONArray().put("1.1.1.1").put("8.8.8.8").put("localhost")));

        // Log to a file so the debug console can show the dial attempts and the
        // exact reason a connection failed. Debug level only when the console
        // turned verbose on; otherwise warnings/errors only (cheap in prod).
        c.put("log", new JSONObject()
                .put("loglevel", DebugLog.isVerbose() ? "debug" : "warning")
                .put("error", errorLog.getAbsolutePath()));

        c.remove("stats");    // stats API needs a matching inbound; we do not use it
        c.remove("policy");
        c.remove("remarks");
        c.remove("id");
        return c.toString();
    }

    /** A short, secret-free description of the outbound for the log. */
    static String summarizeOutbound(JSONObject cfg) {
        try {
            JSONArray outs = cfg.optJSONArray("outbounds");
            if (outs == null) return "?";
            for (int i = 0; i < outs.length(); i++) {
                JSONObject o = outs.optJSONObject(i);
                if (o == null || !"proxy".equals(o.optString("tag"))) continue;
                JSONObject st = o.optJSONObject("streamSettings");
                JSONObject se = o.optJSONObject("settings");
                String net = st == null ? "?" : st.optString("network", "?");
                String sec = st == null ? "?" : st.optString("security", "?");
                String addr = se == null ? "?" : se.optString("address", "?");
                int port = se == null ? 0 : se.optInt("port", 0);
                return cfg.optString("remarks", "server") + " {proto=" + o.optString("protocol")
                        + ", net=" + net + ", sec=" + sec + ", server=" + addr + ":" + port + "}";
            }
        } catch (Throwable ignored) { }
        return cfg.optString("remarks", "server");
    }

    private static boolean waitForPort(String host, int port, int timeoutMs) {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (System.currentTimeMillis() < deadline) {
            try (Socket s = new Socket()) {
                s.connect(new InetSocketAddress(host, port), 300);
                return true;
            } catch (Throwable ignored) {
                try { Thread.sleep(100); } catch (InterruptedException e) { return false; }
            }
        }
        return false;
    }

    private void copyGeoAssets() {
        AssetManager am = ctx.getAssets();
        for (String name : GEO_FILES) {
            File out = new File(datDir, name);
            if (out.exists()) continue;
            try (InputStream in = am.open(name)) {
                copy(in, out);
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
