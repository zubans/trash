package com.healthlogin.app.net;

import android.util.Base64;
import android.util.Log;

import org.json.JSONObject;

import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;

/**
 * Thin, reflection-based wrapper over the prebuilt libXray AAR (XTLS/libXray).
 *
 * <p>Reflection is deliberate: libXray's exported symbol names and signatures
 * drift between releases, and the gomobile class name depends on the build. By
 * probing for the known variants we bind to whatever the dropped-in AAR actually
 * exposes — and, crucially, if the AAR is missing the app still runs, it just
 * never leaves DIRECT mode. Only {@code run}/{@code stop} are required here;
 * per-server probing is done with OkHttp in {@link ChannelManager}.
 *
 * <p>Verify against your release. Class is usually {@code libXray.LibXray};
 * {@code runXray} takes {@code base64(JSON{datDir,configPath,maxMemory})} and
 * returns {@code base64(JSON{success,err})}; {@code stopXray()} takes no args.
 */
final class LibXrayBridge {
    private static final String TAG = "LibXrayBridge";
    private static final String[] CLASS_CANDIDATES = {
            "libXray.LibXray", "libXray.Libxray", "libv2ray.Libv2ray", "go.libXray.LibXray"
    };

    private final Class<?> cls;

    private LibXrayBridge(Class<?> cls) { this.cls = cls; }

    static LibXrayBridge tryLoad() {
        for (String name : CLASS_CANDIDATES) {
            try {
                LibXrayBridge b = new LibXrayBridge(Class.forName(name));
                DebugLog.add(TAG, "bound " + name + " (core " + b.version() + ")");
                return b;
            } catch (Throwable ignored) { }
        }
        DebugLog.add(TAG, "AAR not found on classpath — proxy disabled; drop the release "
                + "LibXray.aar into app/libs/");
        return null;
    }

    String version() {
        Object r = invoke0("xrayVersion", "XrayVersion");
        return r == null ? "?" : String.valueOf(r);
    }

    /** Start an Xray instance from a config file. @return true on success. */
    boolean run(String datDir, String configPath, String configJson) {
        // New API: runXray(base64(JSON)) -> base64(CallResponse)
        try {
            String req = new JSONObject()
                    .put("datDir", datDir)
                    .put("configPath", configPath)
                    .put("maxMemory", 0L)
                    .toString();
            String b64 = Base64.encodeToString(req.getBytes(StandardCharsets.UTF_8), Base64.NO_WRAP);
            String out = invoke1(b64, "runXray", "RunXray");
            if (out != null) return isSuccess(out);
        } catch (Throwable t) {
            Log.w(TAG, "runXray(base64) unavailable, trying legacy", t);
        }
        // Legacy API: runXrayFromJSON(datDir, configJson) -> "" on success
        try {
            Method m = cls.getMethod("runXrayFromJSON", String.class, String.class);
            Object r = m.invoke(null, datDir, configJson);
            return isSuccess(r == null ? "" : String.valueOf(r));
        } catch (NoSuchMethodException ignored) {
        } catch (Throwable t) {
            Log.w(TAG, "runXrayFromJSON failed", t);
        }
        Log.e(TAG, "no known run* method on " + cls.getName());
        return false;
    }

    void stop() {
        if (invoke0("stopXray", "StopXray") == null) {
            Log.w(TAG, "no known stop method on " + cls.getName());
        }
    }

    // --- reflection helpers ---

    private Object invoke0(String... names) {
        for (String n : names) {
            try { return orTrue(cls.getMethod(n).invoke(null)); }
            catch (NoSuchMethodException ignored) { }
            catch (Throwable t) { Log.w(TAG, n + " threw", t); }
        }
        return null;
    }

    private String invoke1(String arg, String... names) {
        for (String n : names) {
            try {
                Object r = cls.getMethod(n, String.class).invoke(null, arg);
                return r == null ? "" : String.valueOf(r);
            } catch (NoSuchMethodException ignored) {
            } catch (Throwable t) { Log.w(TAG, n + " threw", t); }
        }
        return null;
    }

    /** Void methods return null from invoke; normalise to a non-null marker. */
    private static Object orTrue(Object r) { return r == null ? Boolean.TRUE : r; }

    private static boolean isSuccess(String out) {
        if (out == null) return false;
        String json = out.trim();
        // Response may be base64-wrapped.
        try {
            String dec = new String(Base64.decode(out, Base64.DEFAULT), StandardCharsets.UTF_8).trim();
            if (dec.startsWith("{")) json = dec;
        } catch (Throwable ignored) { }
        if (json.isEmpty()) return true; // legacy "" == ok
        try {
            JSONObject o = new JSONObject(json);
            if (o.has("success")) return o.optBoolean("success", false);
            if (o.has("err"))     return o.optString("err", "").isEmpty();
            if (o.has("error"))   return o.optString("error", "").isEmpty();
            if (o.has("code"))    return o.optInt("code", -1) == 0;
        } catch (Throwable ignored) { }
        return false;
    }
}
