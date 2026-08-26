package com.healthlogin.app.net;

/**
 * Static network coordinates for the fallback channel. These mirror the native
 * build's VITE_API_URL; keep them in sync if the backend host changes.
 */
public final class NetConfig {
    public final String apiHost;    // host that gets tunnelled, e.g. "moya-usluga.ru"
    public final String healthUrl;  // direct/proxy health probe target
    public final String configUrl;  // endpoint-list URL, polled every 10 min
    public final int socksPort;     // local libXray SOCKS inbound

    public NetConfig(String apiHost, String healthUrl, String configUrl, int socksPort) {
        this.apiHost = apiHost;
        this.healthUrl = healthUrl;
        this.configUrl = configUrl;
        this.socksPort = socksPort;
    }

    /** Defaults for the production native build (VITE_API_URL=https://moya-usluga.ru). */
    public static NetConfig production() {
        return new NetConfig(
                "moya-usluga.ru",
                "https://moya-usluga.ru/health",
                "https://moya-usluga.ru/api/app/endpoints",
                1080);
    }
}
