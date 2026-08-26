package com.healthlogin.app.net;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Proxy;
import java.net.ProxySelector;
import java.net.SocketAddress;
import java.net.URI;
import java.util.Collections;
import java.util.List;

/**
 * Process-wide state of the covert VLESS fallback channel.
 *
 * <p>The app runs DIRECT by default. When {@link ChannelManager} decides the
 * direct path to the backend is dead, it brings up a local SOCKS proxy (libXray)
 * and flips this channel to PROXY. Two consumers read the state:
 * <ul>
 *   <li>the JVM-default {@link ProxySelector} installed in MainActivity, which is
 *       what Capacitor's HttpURLConnection-based HTTP plugin (axios) goes through;</li>
 *   <li>{@code NativeWebSocketPlugin}, whose OkHttp client uses {@link #proxySelector()}.</li>
 * </ul>
 * Nothing here is surfaced to the WebView/UI: the switch is invisible to the user.
 */
public final class VlessChannel {

    public enum Mode { DIRECT, PROXY }

    private static final VlessChannel INSTANCE = new VlessChannel();
    public static VlessChannel get() { return INSTANCE; }

    private volatile Mode mode = Mode.DIRECT;
    private volatile String socksHost = "127.0.0.1";
    private volatile int socksPort = 1080;
    private volatile String activeRemark = null;
    /** Only traffic to this host (and its subdomains) is tunnelled; everything
     *  else stays direct even in PROXY mode. */
    private volatile String apiHost = "";

    private VlessChannel() {}

    public void configure(String apiHost, String socksHost, int socksPort) {
        this.apiHost = apiHost == null ? "" : apiHost;
        this.socksHost = socksHost;
        this.socksPort = socksPort;
    }

    public Mode mode() { return mode; }
    public boolean isProxy() { return mode == Mode.PROXY; }
    public String activeRemark() { return activeRemark; }
    public int socksPort() { return socksPort; }

    public void setDirect() {
        this.activeRemark = null;
        this.mode = Mode.DIRECT;
    }

    public void setProxy(String remark) {
        this.activeRemark = remark;
        this.mode = Mode.PROXY;
    }

    /** The SOCKS proxy backed by the local libXray inbound. */
    public Proxy socksProxy() {
        return new Proxy(Proxy.Type.SOCKS, new InetSocketAddress(socksHost, socksPort));
    }

    private boolean matchesApiHost(String host) {
        if (host == null || apiHost.isEmpty()) return false;
        String h = host.toLowerCase();
        String a = apiHost.toLowerCase();
        return h.equals(a) || h.endsWith("." + a);
    }

    /**
     * ProxySelector consulted by every HttpURLConnection in the process,
     * including Capacitor's native HTTP layer. Returns the SOCKS proxy only in
     * PROXY mode and only for the backend host; NO_PROXY otherwise.
     */
    public ProxySelector proxySelector() {
        return new ProxySelector() {
            @Override public List<Proxy> select(URI uri) {
                if (mode == Mode.PROXY && uri != null && matchesApiHost(uri.getHost())) {
                    return Collections.singletonList(socksProxy());
                }
                return Collections.singletonList(Proxy.NO_PROXY);
            }
            @Override public void connectFailed(URI uri, SocketAddress sa, IOException e) { /* no-op */ }
        };
    }
}
