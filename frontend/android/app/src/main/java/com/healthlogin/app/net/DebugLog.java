package com.healthlogin.app.net;

import android.util.Log;

import java.text.SimpleDateFormat;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Date;
import java.util.Deque;
import java.util.List;
import java.util.Locale;

/**
 * In-memory ring buffer of channel/proxy diagnostics, surfaced to the WebView's
 * debug console via {@code VpnDebugPlugin}. It records the channel decisions, the
 * health-probe request/response for each attempt, what is stored, and the tail
 * of libXray's own log — everything needed to see why the proxy path did or did
 * not connect. Every line also goes to logcat under the "VlessDebug" tag.
 */
public final class DebugLog {
    private static final int MAX_LINES = 1000;
    private static final Deque<String> LINES = new ArrayDeque<>();
    private static final SimpleDateFormat TS = new SimpleDateFormat("HH:mm:ss.SSS", Locale.US);

    // Verbose mode is switched on by the debug console (VITE_DEBUG builds only).
    // It raises libXray's own log level to "debug"; the in-memory ring buffer is
    // always collected regardless, since it is cheap and bounded.
    private static volatile boolean verbose = false;

    private DebugLog() {}

    public static void setVerbose(boolean on) { verbose = on; }
    public static boolean isVerbose() { return verbose; }

    public static synchronized void add(String tag, String msg) {
        String line = TS.format(new Date()) + " [" + tag + "] " + msg;
        if (LINES.size() >= MAX_LINES) {
            LINES.pollFirst();
        }
        LINES.addLast(line);
        Log.d("VlessDebug", line);
    }

    /** A copy of the current buffer, oldest first. */
    public static synchronized List<String> snapshot() {
        return new ArrayList<>(LINES);
    }

    public static synchronized void clear() {
        LINES.clear();
    }
}
