package com.healthlogin.app.net;

import android.util.Base64;

import org.json.JSONObject;

import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;

/**
 * Wrapper over the prebuilt libXray AAR (XTLS/libXray).
 *
 * <p>This build exposes a single native entry point — {@code libXray.LibXray.invoke(String)} —
 * rather than one exported function per command. The contract is:
 * <pre>
 *   invoke( base64( {"name":"&lt;fn&gt;","data": base64(&lt;paramsJSON&gt;)} ) )
 *     -&gt; {"success":bool, "data": base64(&lt;resultJSON&gt;), "error":"..."}
 * </pre>
 * Commands used here: {@code RunXrayFromJSON} (datDir, configJSON, maxMemory),
 * {@code StopXray}, {@code XrayVersion} (result: coreVersion).
 *
 * <p>Reflection is used only to locate {@code invoke}; if the AAR is absent the
 * app still runs and stays DIRECT.
 */
final class LibXrayBridge {
    private static final String TAG = "LibXrayBridge";

    private final Method invoke; // static String invoke(String)

    private LibXrayBridge(Method invoke) {
        this.invoke = invoke;
    }

    static LibXrayBridge tryLoad() {
        try {
            Class<?> cls = Class.forName("libXray.LibXray");
            Method m = cls.getMethod("invoke", String.class);
            LibXrayBridge b = new LibXrayBridge(m);
            DebugLog.add(TAG, "bound libXray.LibXray.invoke (core " + b.version() + ")");
            return b;
        } catch (Throwable t) {
            DebugLog.add(TAG, "libXray bind failed (" + t.getClass().getSimpleName() + ": "
                    + t.getMessage() + ") — proxy disabled");
            return null;
        }
    }

    /** libXray core version, or "?". Best-effort — only used for logging. */
    String version() {
        String r = call("getCoreVersion", "{}");
        if (r == null) return "?";
        try {
            JSONObject o = new JSONObject(r);
            String v = o.optString("coreVersion", o.optString("version", ""));
            return v.isEmpty() ? "?" : v;
        } catch (Throwable t) {
            return "?";
        }
    }

    /** Start the core from an inline config JSON. @return true on success. */
    boolean run(String datDir, String configJson) {
        try {
            String params = new JSONObject()
                    .put("datDir", datDir)
                    .put("configJSON", configJson)
                    .put("maxMemory", 0L)
                    .toString();
            return call("runXrayFromJson", params) != null;
        } catch (Throwable t) {
            DebugLog.add(TAG, "run marshal failed: " + t.getMessage());
            return false;
        }
    }

    void stop() {
        call("stopXray", "{}");
    }

    // --- invoke plumbing ---

    /**
     * Runs one command through invoke. @return the decoded result JSON on success
     * (possibly empty string), or null on failure (logged).
     */
    private String call(String name, String paramsJson) {
        try {
            // Wire format (from libXray invoke.go): the invoke argument is RAW JSON
            // of {apiVersion,name,data}; the inner "data" is base64 of the specific
            // request JSON (the handler base64-decodes it). The response is
            // {success, data(base64), error}.
            JSONObject req = new JSONObject()
                    .put("apiVersion", 1)
                    .put("name", name)
                    .put("data", b64(paramsJson == null ? "{}" : paramsJson));
            String out = (String) invoke.invoke(null, req.toString());
            if (out == null) {
                DebugLog.add(TAG, name + ": null response");
                return null;
            }
            JSONObject resp;
            try {
                resp = new JSONObject(asJson(out));
            } catch (Throwable pe) {
                DebugLog.add(TAG, name + ": unparseable response: " + trunc(out));
                return null;
            }
            if (!resp.optBoolean("success", false)) {
                String err = resp.optString("error", resp.optString("err", "unknown"));
                DebugLog.add(TAG, name + " failed: " + err + " | raw=" + trunc(out));
                return null;
            }
            String data = resp.optString("data", "");
            return data.isEmpty() ? "" : asJson(data);
        } catch (Throwable t) {
            DebugLog.add(TAG, name + " invoke threw: " + t.getClass().getSimpleName() + ": " + t.getMessage());
            return null;
        }
    }

    private static String trunc(String s) {
        if (s == null) return "null";
        s = s.replace('\n', ' ');
        return s.length() > 240 ? s.substring(0, 240) + "…" : s;
    }

    private static String b64(String s) {
        return Base64.encodeToString(s.getBytes(StandardCharsets.UTF_8), Base64.NO_WRAP);
    }

    /** Return s as a JSON string: raw if it already looks like JSON, else base64-decoded. */
    private static String asJson(String s) {
        String t = s.trim();
        if (t.startsWith("{") || t.startsWith("[")) return t;
        try {
            return new String(Base64.decode(s, Base64.DEFAULT), StandardCharsets.UTF_8);
        } catch (Throwable e) {
            return t;
        }
    }
}
